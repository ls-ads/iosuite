package iocore

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// UpscaleProvider defines the types of upscaling backends supported.
type UpscaleProvider string

const (
	ProviderLocalCPU  UpscaleProvider = "local_cpu"
	ProviderLocalGPU  UpscaleProvider = "local_gpu"
	ProviderReplicate UpscaleProvider = "replicate"
	ProviderRunPod    UpscaleProvider = "runpod"
)

// GetRunPodEndpointName returns the endpoint name prefix for a given model
func GetRunPodEndpointName(model string) string {
	if model == "ffmpeg" {
		return "iosuite-ffmpeg"
	}
	if model == "real-esrgan" || model == "" {
		return "iosuite-img-real-esrgan"
	}
	return "iosuite-img-" + model
}

// RunPodStatusUpdate provides progress information during RunPod job execution.
type RunPodStatusUpdate struct {
	Phase   string        // "infrastructure", "queued", "in_progress", "completed"
	Message string        // Human-readable status message
	Elapsed time.Duration // Time elapsed since job submission
}

// UpscaleConfig holds configuration for the upscaler.
type UpscaleConfig struct {
	Provider       UpscaleProvider
	APIKey         string
	Model          string                   // Model name (e.g., "real-esrgan")
	OutputFormat   string                   // Output format: "jpg" or "png"
	StatusCallback func(RunPodStatusUpdate) // Optional callback for progress updates
}

type Upscaler interface {
	Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error)
	Rate() float64
	IsActive() bool
}

// NewUpscaler returns an Upscaler implementation based on the provided config.
func NewUpscaler(ctx context.Context, config *UpscaleConfig) (Upscaler, error) {
	switch config.Provider {
	case ProviderLocalCPU, ProviderLocalGPU:
		if config.Model == "ffmpeg" {
			return &ffmpegUpscaler{config: config}, nil
		}
		return &localUpscaler{config: config}, nil
	case ProviderReplicate:
		return &replicateUpscaler{config: config}, nil
	case ProviderRunPod:
		if config.Model != "real-esrgan" && config.Model != "ffmpeg" && config.Model != "" {
			return nil, fmt.Errorf("model not supported for RunPod upscaling: %s", config.Model)
		}

		key := config.APIKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return nil, fmt.Errorf("runpod API key is required (set via -k or RUNPOD_API_KEY)")
		}

		if config.StatusCallback != nil {
			config.StatusCallback(RunPodStatusUpdate{Phase: "infrastructure", Message: "Checking RunPod endpoints..."})
		}

		endpointName := GetRunPodEndpointName(config.Model)
		endpoints, err := GetRunPodEndpoints(ctx, key, endpointName)
		if err != nil {
			return nil, fmt.Errorf("failed to search for runpod infrastructure: %v", err)
		}

		if len(endpoints) == 0 {
			return nil, fmt.Errorf("no runpod endpoint found for model '%s' (endpoint prefix '%s'). please run 'ioimg upscale init -p runpod -m %s' first", config.Model, endpointName, config.Model)
		}

		if config.StatusCallback != nil {
			config.StatusCallback(RunPodStatusUpdate{Phase: "infrastructure", Message: "Found existing RunPod endpoint"})
		}

		rate := CalculateRunPodEndpointRate(endpoints[0].GPUTypeIDs, endpoints[0].WorkersMin)
		isActive := endpoints[0].WorkersMin > 0

		return &runpodUpscaler{config: config, endpointID: endpoints[0].ID, rate: rate, isActive: isActive}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}

// Stubs for implementations

type localUpscaler struct {
	config *UpscaleConfig
}

func (u *localUpscaler) Rate() float64  { return 0.0 }
func (u *localUpscaler) IsActive() bool { return false }

func (u *localUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Upscaling locally", "model", u.config.Model)
	start := time.Now()
	if u.config.Model != "real-esrgan" && u.config.Model != "" {
		return 0, fmt.Errorf("model not supported: %s", u.config.Model)
	}

	// Both local and runpod implementations now assume scale 4 for realism & consistency
	args := []string{"-i", "-", "-o", "-", "-s", "4"}
	if u.config.OutputFormat != "" {
		args = append(args, "-f", u.config.OutputFormat)
	}
	cmd := exec.CommandContext(ctx, "realesrgan-ncnn-vulkan", args...)
	cmd.Stdin = r
	cmd.Stdout = w

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return 0, fmt.Errorf("local upscale failed: %v, stderr: %s", err, stderr.String())
	}
	return time.Since(start), nil
}

type replicateUpscaler struct {
	config *UpscaleConfig
}

func (u *replicateUpscaler) Rate() float64  { return 0.000225 }
func (u *replicateUpscaler) IsActive() bool { return false }

func (u *replicateUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Upscaling via Replicate", "model", u.config.Model)
	key := u.config.APIKey
	if key == "" {
		key = os.Getenv("REPLICATE_API_KEY")
	}
	if key == "" {
		return 0, fmt.Errorf("replicate API key is required (set via -k or REPLICATE_API_KEY)")
	}

	if u.config.Model != "real-esrgan" && u.config.Model != "" {
		return 0, fmt.Errorf("model not supported: %s", u.config.Model)
	}

	// 1. Convert to Base64 (Data URI)
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return 0, err
	}

	url := "https://api.replicate.com/v1/models/nightmareai/real-esrgan/predictions"
	prediction, err := RunReplicatePrediction(ctx, key, url, map[string]interface{}{
		"image": fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(buf.Bytes())),
		"scale": 4,
	})
	if err != nil {
		return 0, err
	}

	// 3. Download result
	var outputURL string
	switch out := prediction.Output.(type) {
	case string:
		outputURL = out
	case []interface{}:
		if len(out) > 0 {
			outputURL = out[0].(string)
		}
	}

	if outputURL == "" {
		return 0, fmt.Errorf("no output URL found in Replicate response")
	}

	imgResp, err := http.Get(outputURL)
	if err != nil {
		return 0, err
	}
	defer imgResp.Body.Close()

	_, err = io.Copy(w, imgResp.Body)
	billableTime := time.Duration(prediction.Metrics.PredictTime * float64(time.Second))
	return billableTime, err
}

type runpodUpscaler struct {
	config     *UpscaleConfig
	endpointID string
	rate       float64
	isActive   bool
}

func (u *runpodUpscaler) Rate() float64  { return u.rate }
func (u *runpodUpscaler) IsActive() bool { return u.isActive }

func (u *runpodUpscaler) emitStatus(phase, message string, elapsed time.Duration) {
	if u.config.StatusCallback != nil {
		u.config.StatusCallback(RunPodStatusUpdate{
			Phase:   phase,
			Message: message,
			Elapsed: elapsed,
		})
	}
}

func (u *runpodUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	key := u.config.APIKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}

	switch u.config.Model {
	case "real-esrgan", "":
		// Only x4plus is supported currently by the underlying container
	default:
		return 0, fmt.Errorf("model not supported: %s", u.config.Model)
	}

	// 1. Convert to Base64
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return 0, err
	}

	// 2. Submit via /runsync — blocks until result is ready, zero polling overhead
	var inputPayload map[string]interface{}
	if u.config.Model == "ffmpeg" {
		inputPayload = map[string]interface{}{
			"input_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
			"ffmpeg_args":  "scale=iw*4:ih*4:flags=lanczos",
			"output_ext":   u.config.OutputFormat,
		}
		if inputPayload["output_ext"] == "" {
			inputPayload["output_ext"] = "png"
		}
	} else {
		inputPayload = map[string]interface{}{
			"image_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
		}
		if u.config.OutputFormat != "" {
			inputPayload["output_format"] = u.config.OutputFormat
		}
	}

	job, err := RunRunPodJobSync(ctx, key, u.endpointID, inputPayload, u.emitStatus)
	if err != nil {
		return 0, err
	}

	// 4. Decode base64 image from output
	var base64Out string
	if u.config.Model == "ffmpeg" {
		base64Out = job.Output.OutputBase64
		if base64Out == "" {
			base64Out = job.Output.ImageBase64 // Fallback
		}
	} else {
		base64Out = job.Output.ImageBase64
	}

	if base64Out == "" {
		return 0, fmt.Errorf("runpod worker returned no data in output (status: %s)", job.Status)
	}

	decoded, err := base64.StdEncoding.DecodeString(job.Output.ImageBase64)
	if err != nil {
		return 0, fmt.Errorf("failed to decode output image: %v", err)
	}

	_, err = w.Write(decoded)
	billableTime := time.Duration(job.ExecutionTime) * time.Millisecond
	return billableTime, err
}

type ffmpegUpscaler struct {
	config *UpscaleConfig
}

func (u *ffmpegUpscaler) Rate() float64  { return 0.0 }
func (u *ffmpegUpscaler) IsActive() bool { return false }

func (u *ffmpegUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Upscaling via FFmpeg (Lanczos)", "model", u.config.Model, "provider", u.config.Provider)
	start := time.Now()

	isGPU := u.config.Provider == ProviderLocalGPU || u.config.Provider == ""

	// 4x upscale using Lanczos filter
	args := []string{
		"-hide_banner", "-loglevel", "error",
	}

	if isGPU {
		if runtime.GOOS == "darwin" {
			args = append(args, "-hwaccel", "videotoolbox")
		} else {
			args = append(args, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		}
	}

	args = append(args, "-i", "-")

	filter := "scale=iw*4:ih*4:flags=lanczos"
	if isGPU {
		if runtime.GOOS == "darwin" {
			// Metal/VideoToolbox doesn't have a direct scale_npp equivalent here,
			// but we can scale via CPU lanczos or another filter if needed.
			// For now, lanczos is high quality enough.
			filter = "scale=iw*4:ih*4:flags=lanczos"
		} else {
			filter = "scale_npp=iw*4:ih*4:interp_algo=lanczos"
		}
	}
	args = append(args, "-vf", filter)

	args = append(args, "-f", "image2", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = r
	cmd.Stdout = w

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg upscale failed: %v, stderr: %s", err, stderr.String())
	}

	return time.Since(start), nil
}
