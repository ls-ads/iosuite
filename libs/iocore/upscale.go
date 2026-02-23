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
	"time"
)

// UpscaleProvider defines the types of upscaling backends supported.
type UpscaleProvider string

const (
	ProviderLocal     UpscaleProvider = "local"
	ProviderReplicate UpscaleProvider = "replicate"
	ProviderRunPod    UpscaleProvider = "runpod"
)

// GetRunPodEndpointName returns the endpoint name prefix for a given model
func GetRunPodEndpointName(model string) string {
	if model == "real-esrgan" || model == "" {
		return "ioimg-real-esrgan"
	}
	return "ioimg-" + model
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
	StatusCallback func(RunPodStatusUpdate) // Optional callback for progress updates
}

// Upscaler is the interface for image upscaling operations.
type Upscaler interface {
	Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error)
}

// NewUpscaler returns an Upscaler implementation based on the provided config.
func NewUpscaler(ctx context.Context, config UpscaleConfig) (Upscaler, error) {
	switch config.Provider {
	case ProviderLocal:
		return &localUpscaler{config: config}, nil
	case ProviderReplicate:
		return &replicateUpscaler{config: config}, nil
	case ProviderRunPod:
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

		return &runpodUpscaler{config: config, endpointID: endpoints[0].ID}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}

// Stubs for implementations

type localUpscaler struct {
	config UpscaleConfig
}

func (u *localUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Upscaling locally", "model", u.config.Model)
	start := time.Now()
	if u.config.Model != "real-esrgan" && u.config.Model != "" {
		return 0, fmt.Errorf("model not supported: %s", u.config.Model)
	}

	// Both local and runpod implementations now assume scale 4 for realism & consistency
	cmd := exec.CommandContext(ctx, "realesrgan-ncnn-vulkan", "-i", "-", "-o", "-", "-s", "4")
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
	config UpscaleConfig
}

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
	config     UpscaleConfig
	endpointID string
}

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

	jobID, err := SubmitRunPodJob(ctx, key, u.endpointID, map[string]interface{}{
		"image_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
	if err != nil {
		return 0, err
	}

	// 3. Poll /status/{jobId} until COMPLETED or FAILED
	job, err := PollRunPodJob(ctx, key, u.endpointID, jobID, u.emitStatus)
	if err != nil {
		return 0, err
	}

	// 4. Decode base64 image from output
	if job.Output.ImageBase64 == "" {
		return 0, fmt.Errorf("runpod worker returned no image in output (status: %s)", job.Output.Status)
	}

	decoded, err := base64.StdEncoding.DecodeString(job.Output.ImageBase64)
	if err != nil {
		return 0, fmt.Errorf("failed to decode output image: %v", err)
	}

	_, err = w.Write(decoded)
	billableTime := time.Duration(job.ExecutionTime) * time.Millisecond
	return billableTime, err
}
