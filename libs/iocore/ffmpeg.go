package iocore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FFmpegConfig holds configuration for FFmpeg execution.
type FFmpegConfig struct {
	Provider       UpscaleProvider
	APIKey         string
	Model          string // Default "ffmpeg"
	StatusCallback func(RunPodStatusUpdate)
	Volume         string   // RunPod volume ID or size in GB
	GPUID          string   // Requested GPU type
	DataCenterIDs  []string // Preferred data centers
	KeepFailed     bool
}

// RunFFmpegAction executes an FFmpeg command with the given input, output, filter, and extra arguments.
func RunFFmpegAction(ctx context.Context, config *FFmpegConfig, input string, output string, filter string, extraArgs []string) error {
	p := ProviderLocalGPU
	if config != nil && config.Provider != "" {
		p = config.Provider
	}

	if p == ProviderLocalCPU || p == ProviderLocalGPU {
		return runLocalFFmpeg(ctx, p, input, output, filter, extraArgs)
	}

	if p == ProviderRunPod {
		if config.Volume != "" {
			return runRunPodVolumeFFmpeg(ctx, config, input, output, filter, extraArgs)
		}
		return runRunPodFFmpeg(ctx, config, input, output, filter, extraArgs)
	}

	return fmt.Errorf("unsupported provider: %s", p)
}

func runRunPodVolumeFFmpeg(ctx context.Context, config *FFmpegConfig, input, output, filter string, extraArgs []string) error {
	Info("Running FFmpeg on RunPod via Volume Workflow", "input", input, "volume", config.Volume)

	// 1. Resolve Model Config (Template + GPUs)
	gpuIDs := config.GPUID
	if gpuIDs == "" {
		gpuIDs = "NVIDIA RTX A4000" // Default
	}

	var templateID string
	if config.Model == "ffmpeg" || config.Model == "" {
		templateID = "uduo7jdyhn"
	} else if config.Model == "real-esrgan" {
		templateID = "047z8w5i69"
	} else {
		return fmt.Errorf("unsupported model for Volume workflow: %s", config.Model)
	}

	// 2. Prepare Workflow Config
	volWorkflowCfg := VolumeWorkflowConfig{
		APIKey:         config.APIKey,
		TemplateID:     templateID,
		GPUID:          gpuIDs,
		InputLocalPath: input,
		OutputLocalDir: filepath.Dir(output),
		KeepFailed:     config.KeepFailed,
	}

	if len(config.DataCenterIDs) > 0 {
		volWorkflowCfg.Region = config.DataCenterIDs[0]
	} else {
		volWorkflowCfg.Region = "EU-RO-1" // Default
	}

	// Parse Volume ID or Size
	if size, err := strconv.Atoi(config.Volume); err == nil {
		volWorkflowCfg.VolumeSizeGB = size
	} else {
		volWorkflowCfg.VolumeID = config.Volume
	}

	// 3. Execution wrapper
	statusFunc := func(phase, message string) {
		if config.StatusCallback != nil {
			config.StatusCallback(RunPodStatusUpdate{Phase: phase, Message: message})
		}
	}

	volWorkflowCfg.FFmpegArgs = strings.Join(extraArgs, ",")
	if filter != "" {
		if volWorkflowCfg.FFmpegArgs != "" {
			volWorkflowCfg.FFmpegArgs = "-vf," + filter + "," + volWorkflowCfg.FFmpegArgs
		} else {
			volWorkflowCfg.FFmpegArgs = "-vf," + filter
		}
	}
	volWorkflowCfg.OutputExt = strings.TrimPrefix(filepath.Ext(output), ".")

	err := RunPodServerlessVolumeWorkflow(ctx, volWorkflowCfg, statusFunc)
	if err != nil {
		return err
	}

	// The workflow downloads the results to OutputLocalDir.
	// We might need to rename the file if OutputLocalDir contains something else.
	// For now, we assume the output file name in S3 matches the expected local base name.

	return nil
}

func runLocalFFmpeg(ctx context.Context, provider UpscaleProvider, input string, output string, filter string, extraArgs []string) error {
	// Base command
	args := []string{"-hide_banner", "-loglevel", "error"}

	isGPU := provider == ProviderLocalGPU

	if isGPU {
		// Hardware acceleration for decoding
		if runtime.GOOS == "darwin" {
			args = append(args, "-hwaccel", "videotoolbox")
		} else {
			// Windows/Linux assume CUDA if GPU provider is selected
			args = append(args, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		}
	}

	// Handle input
	args = append(args, "-i", input)

	// Inject the filter if provided
	if filter != "" {
		f := filter
		if isGPU {
			// Effort to use CUDA optimized filters if possible
			// This is a naive replacement, but it's a start for "zero-copy"
			// Note: not all filters have _cuda variants, so we cautiously replace common ones
			f = strings.ReplaceAll(f, "scale=", "scale_npp=")
			f = strings.ReplaceAll(f, "transpose=", "transpose_npp=")
		}
		args = append(args, "-vf", f)
	}

	// Add extra args
	args = append(args, extraArgs...)

	// Encoding optimization
	if isGPU {
		if IsVideo(output) {
			if runtime.GOOS == "darwin" {
				// Use VideoToolbox for macOS
				args = append(args, "-c:v", "h264_videotoolbox", "-b:v", "5M")
			} else {
				// Use NVENC for video encoding on Windows/Linux
				args = append(args, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "hq")
			}
		}
	}

	// Always overwrite
	args = append(args, "-y", output)

	if err := RunBinary(ctx, "ffmpeg-serve", args, nil, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("ffmpeg failed (provider: %s): %v", provider, err)
	}
	return nil
}

func runRunPodFFmpeg(ctx context.Context, config *FFmpegConfig, input string, output string, filter string, extraArgs []string) error {
	Info("Running FFmpeg on RunPod", "input", input)
	key := config.APIKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = "ffmpeg"
	}

	// 1. Prepare endpoints
	endpointName := "io-ffmpeg"
	if model != "ffmpeg" {
		endpointName = "io-" + model
	}

	endpoints, err := GetRunPodEndpoints(ctx, key, endpointName)
	if err != nil {
		return err
	}
	if len(endpoints) == 0 {
		return fmt.Errorf("no runpod endpoint found for ffmpeg (prefix '%s'). please run 'ioimg/iovid init' first", endpointName)
	}
	endpointID := endpoints[0].ID

	// 2. Read input file and encode to base64
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("failed to read input file: %v", err)
	}

	// 3. Construct ffmpeg_args for the handler
	// The handler expects a comma-separated string or something similar?
	// Based on handler.py: url = f"http://localhost:8080/process?args={ffmpeg_args}"
	// And process_media: url = f"http://localhost:8080/process?args={ffmpeg_args}"
	// ffmpeg-serve expects arguments exactly as they would be passed to CLI.

	actionArgs := []string{}
	if filter != "" {
		actionArgs = append(actionArgs, "-vf", filter)
	}
	actionArgs = append(actionArgs, extraArgs...)

	// Join with commas as many of these handlers use comma as separator for query params
	ffmpegArgsStr := strings.Join(actionArgs, ",")

	inputPayload := map[string]interface{}{
		"input_base64": base64.StdEncoding.EncodeToString(data),
		"ffmpeg_args":  ffmpegArgsStr,
		"output_ext":   strings.TrimPrefix(filepath.Ext(output), "."),
	}

	// 4. Submit job
	job, err := RunRunPodJobSync(ctx, key, endpointID, inputPayload, func(phase, message string, elapsed time.Duration) {
		if config.StatusCallback != nil {
			config.StatusCallback(RunPodStatusUpdate{Phase: phase, Message: message, Elapsed: elapsed})
		}
	})
	if err != nil {
		return err
	}

	// 5. Decode output
	var base64Out string
	if out, ok := job.Output["output_base64"].(string); ok {
		base64Out = out
	} else if out, ok := job.Output["image_base64"].(string); ok {
		base64Out = out
	}

	if base64Out == "" {
		return fmt.Errorf("runpod worker returned no output data")
	}

	decoded, err := base64.StdEncoding.DecodeString(base64Out)
	if err != nil {
		return fmt.Errorf("failed to decode output: %v", err)
	}

	if err := os.WriteFile(output, decoded, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %v", err)
	}

	return nil
}

// Geometric Transformations

func Scale(ctx context.Context, config *FFmpegConfig, input, output string, width, height int) error {
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Crop(ctx context.Context, config *FFmpegConfig, input, output string, w, h, x, y int) error {
	filter := fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Rotate(ctx context.Context, config *FFmpegConfig, input, output string, degrees int) error {
	var filter string
	switch degrees {
	case 90:
		filter = "transpose=1"
	case 180:
		filter = "transpose=1,transpose=1"
	case 270:
		filter = "transpose=2"
	default:
		filter = fmt.Sprintf("rotate=%d*PI/180", degrees)
	}
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Flip(ctx context.Context, config *FFmpegConfig, input, output string, axis string) error {
	var filter string
	if axis == "v" {
		filter = "vflip"
	} else {
		filter = "hflip"
	}
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Pad(ctx context.Context, config *FFmpegConfig, input, output string, aspect string) error {
	// Example aspect "16:9"
	// pad=ih*16/9:ih:(ow-iw)/2:(oh-ih)/2
	parts := strings.Split(aspect, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid aspect ratio: %s", aspect)
	}
	filter := fmt.Sprintf("pad=ih*%s/%s:ih:(ow-iw)/2:(oh-ih)/2", parts[0], parts[1])
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

// Visual & Quality Adjustments

func Brighten(ctx context.Context, config *FFmpegConfig, input, output string, level float64) error {
	filter := fmt.Sprintf("eq=brightness=%f", level)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Contrast(ctx context.Context, config *FFmpegConfig, input, output string, level float64) error {
	// -100 to 100 -> eq=contrast=N
	// FFmpeg contrast is 0.0 to 10.0, default 1.0
	// Map -100:0.0, 0:1.0, 100:2.0 (approx)
	val := 1.0 + (level / 100.0)
	filter := fmt.Sprintf("eq=contrast=%f", val)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Saturate(ctx context.Context, config *FFmpegConfig, input, output string, level float64) error {
	filter := fmt.Sprintf("eq=saturation=%f", level)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Denoise(ctx context.Context, config *FFmpegConfig, input, output string, preset string) error {
	var filter string
	switch preset {
	case "weak":
		filter = "hqdn3d=2:2:3:3"
	case "med":
		filter = "hqdn3d=4:4:6:6"
	case "strong":
		filter = "hqdn3d=6:6:9:9"
	default:
		filter = "hqdn3d"
	}
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Sharpen(ctx context.Context, config *FFmpegConfig, input, output string, amount float64) error {
	filter := fmt.Sprintf("unsharp=5:5:%f:5:5:0", amount)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

// Temporal & Stream Operations

func Trim(ctx context.Context, config *FFmpegConfig, input, output string, start, end string) error {
	extraArgs := []string{"-ss", start, "-to", end, "-c", "copy"}
	return RunFFmpegAction(ctx, config, input, output, "", extraArgs)
}

func Transcode(ctx context.Context, config *FFmpegConfig, input, output, vcodec, acodec, vbitrate, abitrate, crf string) error {
	var extraArgs []string

	p := ProviderLocalGPU
	if config != nil && config.Provider != "" {
		p = config.Provider
	}
	isGPU := p == ProviderLocalGPU

	// Video Codec
	if vcodec != "" {
		resolvedVCodec := vcodec
		if isGPU {
			switch vcodec {
			case "h264":
				if runtime.GOOS == "darwin" {
					resolvedVCodec = "h264_videotoolbox"
				} else {
					resolvedVCodec = "h264_nvenc"
					extraArgs = append(extraArgs, "-preset", "p4", "-tune", "hq")
				}
			case "hevc":
				if runtime.GOOS == "darwin" {
					resolvedVCodec = "hevc_videotoolbox"
				} else {
					resolvedVCodec = "hevc_nvenc"
					extraArgs = append(extraArgs, "-preset", "p4", "-tune", "hq")
				}
			case "av1":
				if runtime.GOOS == "darwin" {
					// VideoToolbox AV1 encoding is only on very recent Macs (M3+), fallback to standard if needed
				} else {
					resolvedVCodec = "av1_nvenc"
					extraArgs = append(extraArgs, "-preset", "p4", "-tune", "hq")
				}
			}
		} else {
			// CPU Encoders
			switch vcodec {
			case "h264":
				resolvedVCodec = "libx264"
			case "hevc":
				resolvedVCodec = "libx265"
			case "av1":
				resolvedVCodec = "libsvtav1"
				extraArgs = append(extraArgs, "-preset", "6") // Good default for SVT-AV1
			case "vp9":
				resolvedVCodec = "libvpx-vp9"
			}
		}

		extraArgs = append(extraArgs, "-c:v", resolvedVCodec)
	} else {
		extraArgs = append(extraArgs, "-c:v", "copy")
	}

	// Audio Codec
	if acodec != "" {
		extraArgs = append(extraArgs, "-c:a", acodec)
	} else {
		extraArgs = append(extraArgs, "-c:a", "copy")
	}

	if vbitrate != "" {
		extraArgs = append(extraArgs, "-b:v", vbitrate)
	}
	if abitrate != "" {
		extraArgs = append(extraArgs, "-b:a", abitrate)
	}
	if crf != "" {
		extraArgs = append(extraArgs, "-crf", crf)
	}

	// We can't use RunFFmpegAction for transcode because it forces -hwaccel cuda
	// which applies to all inputs, and some codecs are output-only (ffmpeg-serve
	// chokes on "Option hwaccel cannot be applied to output url").
	// We'll execute RunBinary directly.

	args := []string{"-hide_banner", "-loglevel", "error"}

	if isGPU {
		if runtime.GOOS == "darwin" {
			args = append(args, "-hwaccel", "videotoolbox")
		} else {
			args = append(args, "-hwaccel", "cuda")
			// Remove -hwaccel_output_format cuda since we are changing codecs and might need software filters/scaling beforehand.
		}
	}

	args = append(args, "-i", input)
	args = append(args, extraArgs...)
	args = append(args, "-y", output)

	return RunBinary(ctx, "ffmpeg-serve", args, nil, os.Stdout, os.Stderr)
}

func FPS(ctx context.Context, config *FFmpegConfig, input, output string, rate int) error {
	filter := fmt.Sprintf("fps=fps=%d", rate)
	return RunFFmpegAction(ctx, config, input, output, filter, nil)
}

func Mute(ctx context.Context, config *FFmpegConfig, input, output string) error {
	extraArgs := []string{"-an"}
	return RunFFmpegAction(ctx, config, input, output, "", extraArgs)
}

func Speed(ctx context.Context, config *FFmpegConfig, input, output string, multiplier float64) error {
	// Video speed: setpts=1/multiplier*PTS
	// Audio speed: atempo=multiplier
	videoFilter := fmt.Sprintf("setpts=%f*PTS", 1.0/multiplier)
	extraArgs := []string{"-filter_complex", fmt.Sprintf("[0:v]%s[v];[0:a]atempo=%f[a]", videoFilter, multiplier), "-map", "[v]", "-map", "[a]"}
	return RunFFmpegAction(ctx, config, input, output, "", extraArgs)
}

func GetVideoDuration(ctx context.Context, input string) (float64, error) {
	info, err := GetMediaInfo(ctx, input)
	if err != nil {
		return 0, err
	}
	if len(info.Streams) == 0 {
		return 0, fmt.Errorf("no streams found in video")
	}
	// Try finding the duration on the container (format block) or the first video stream
	durationStr := info.Format.Duration
	if durationStr == "" {
		for _, s := range info.Streams {
			if s.Duration != "" {
				durationStr = s.Duration
				break
			}
		}
	}
	if durationStr == "" {
		return 0, fmt.Errorf("could not determine video duration")
	}

	v, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration %q: %v", durationStr, err)
	}
	return v, nil
}

// ProbeOutput maps the JSON output of ffprobe.
type ProbeOutput struct {
	Streams []Stream `json:"streams"`
	Format  Format   `json:"format"`
}

type Format struct {
	Duration string `json:"duration"`
}

// Stream represents an individual media stream inside a file.
type Stream struct {
	Index     int    `json:"index"`
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Duration  string `json:"duration"`
}

// GetMediaInfo executes ffprobe on the input file and returns parsed JSON metadata.
func GetMediaInfo(ctx context.Context, input string) (*ProbeOutput, error) {
	var out bytes.Buffer
	args := []string{
		"-v", "error",
		"-show_streams",
		"-show_format",
		"-print_format", "json",
		input,
	}

	err := RunBinary(ctx, "ffprobe", args, nil, &out, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %v", err)
	}

	var parsed ProbeOutput
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe json: %v", err)
	}

	return &parsed, nil
}

func Concat(ctx context.Context, config *FFmpegConfig, inputs []string, output string) error {
	if len(inputs) < 2 {
		return fmt.Errorf("concat requires at least 2 input files")
	}

	// 1. Extract and verify metadata of the first file
	baseInfo, err := GetMediaInfo(ctx, inputs[0])
	if err != nil {
		return fmt.Errorf("failed to probe first file '%s': %v", inputs[0], err)
	}

	var baseVCodec, baseACodec string
	var baseWidth, baseHeight int

	for _, s := range baseInfo.Streams {
		if s.CodecType == "video" {
			baseVCodec = s.CodecName
			baseWidth = s.Width
			baseHeight = s.Height
		} else if s.CodecType == "audio" {
			baseACodec = s.CodecName
		}
	}

	if baseVCodec == "" {
		return fmt.Errorf("could not find a video stream in the first file '%s'", inputs[0])
	}

	// 2. Iterate through remaining files and strictly guarantee metadata matches
	for i := 1; i < len(inputs); i++ {
		file := inputs[i]
		info, err := GetMediaInfo(ctx, file)
		if err != nil {
			return fmt.Errorf("failed to probe file '%s': %v", file, err)
		}

		var vCodec, aCodec string
		var height, width int

		for _, s := range info.Streams {
			if s.CodecType == "video" {
				vCodec = s.CodecName
				width = s.Width
				height = s.Height
			} else if s.CodecType == "audio" {
				aCodec = s.CodecName
			}
		}

		if vCodec != baseVCodec {
			return fmt.Errorf("incompatible video codecs: '%s' has %s, but '%s' has %s. Please 'iovid transcode' them to the same codec first", inputs[0], baseVCodec, file, vCodec)
		}
		if width != baseWidth || height != baseHeight {
			return fmt.Errorf("incompatible resolutions: '%s' is %dx%d, but '%s' is %dx%d. Please 'iovid scale' them to match first", inputs[0], baseWidth, baseHeight, file, width, height)
		}
		if aCodec != baseACodec {
			return fmt.Errorf("incompatible audio codecs: '%s' has %s, but '%s' has %s. Please 'iovid transcode' them to the same codec first", inputs[0], baseACodec, file, aCodec)
		}
	}

	// 3. Create the intermediate concat list file
	tmpFile, err := os.CreateTemp("", "ffmpeg_concat_*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp concat file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	var b bytes.Buffer
	for _, file := range inputs {
		// Needs to handle potentially absolute or relative paths with FFmpeg quotes
		absFile, err := filepath.Abs(file)
		if err != nil {
			absFile = file
		}
		// Write format: file '/path/to/file.mp4'
		b.WriteString(fmt.Sprintf("file '%s'\n", strings.ReplaceAll(absFile, "'", "'\\''")))
	}

	if _, err := tmpFile.Write(b.Bytes()); err != nil {
		return fmt.Errorf("failed to write to temp concat file: %v", err)
	}
	tmpFile.Close()

	// 4. Execute the lossless concatenation
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "concat",
		"-safe", "0",
		"-i", tmpFile.Name(),
		"-c", "copy",
		"-y", output,
	}

	return RunBinary(ctx, "ffmpeg-serve", args, nil, os.Stdout, os.Stderr)
}

func Chunk(ctx context.Context, input, outputPattern string, chunks int, length float64) error {
	segmentTime := length
	if chunks > 0 {
		duration, err := GetVideoDuration(ctx, input)
		if err != nil {
			return err
		}
		if duration <= 0 {
			return fmt.Errorf("could not determine video duration")
		}
		segmentTime = duration / float64(chunks)
	}
	if segmentTime <= 0 {
		return fmt.Errorf("invalid chunk length: %f", segmentTime)
	}

	args := []string{
		"-v", "error",
		"-i", input,
		"-c", "copy",
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%f", segmentTime),
		"-reset_timestamps", "1",
		"-y", outputPattern,
	}

	return RunBinary(ctx, "ffmpeg-serve", args, nil, os.Stdout, os.Stderr)
}
