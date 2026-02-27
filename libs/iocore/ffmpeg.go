package iocore

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// FFmpegConfig holds configuration for FFmpeg execution.
type FFmpegConfig struct {
	Provider       UpscaleProvider
	APIKey         string
	Model          string // Default "ffmpeg"
	StatusCallback func(RunPodStatusUpdate)
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
		return runRunPodFFmpeg(ctx, config, input, output, filter, extraArgs)
	}

	return fmt.Errorf("unsupported provider: %s", p)
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

	if err := RunBinary(ctx, "ffmpeg", args, nil, os.Stdout, os.Stderr); err != nil {
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
	// 5. Decode output
	var base64Out string
	if job.Output.OutputBase64 != "" {
		base64Out = job.Output.OutputBase64
	} else if job.Output.ImageBase64 != "" {
		base64Out = job.Output.ImageBase64
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
