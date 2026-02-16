package iocore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

const (
	// Opinionated Endpoint IDs / Versions
	RunPodRealESRGANEndpoint = "j58z8n4u2k6k0m" // Standardized endpoint for iosuite
)

// UpscaleConfig holds configuration for the upscaler.
type UpscaleConfig struct {
	Provider UpscaleProvider
	APIKey   string
	Model    string // Model name (e.g., "real-esrgan")
	Scale    int    // e.g., 2, 4
}

// Upscaler is the interface for image upscaling operations.
type Upscaler interface {
	Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error)
}

// NewUpscaler returns an Upscaler implementation based on the provided config.
func NewUpscaler(config UpscaleConfig) (Upscaler, error) {
	switch config.Provider {
	case ProviderLocal:
		return &localUpscaler{config: config}, nil
	case ProviderReplicate:
		return &replicateUpscaler{config: config}, nil
	case ProviderRunPod:
		return &runpodUpscaler{config: config}, nil
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

	cmd := exec.CommandContext(ctx, "realesrgan-ncnn-vulkan", "-i", "-", "-o", "-", "-s", fmt.Sprintf("%d", u.config.Scale))
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

type replicatePredictionRequest struct {
	Input map[string]interface{} `json:"input"`
}

type replicatePredictionResponse struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"`
	Output  interface{}       `json:"output"`
	Error   string            `json:"error"`
	Urls    map[string]string `json:"urls"`
	Version string            `json:"version"`
	Metrics struct {
		PredictTime float64 `json:"predict_time"`
	} `json:"metrics"`
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

	reqBody := replicatePredictionRequest{
		Input: map[string]interface{}{
			"image": fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(buf.Bytes())),
			"scale": u.config.Scale,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	url := "https://api.replicate.com/v1/models/nightmareai/real-esrgan/predictions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Token "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "wait")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("replicate creation failed: %s, body: %s", resp.Status, string(body))
	}

	var prediction replicatePredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&prediction); err != nil {
		return 0, err
	}

	if prediction.Status == "failed" {
		return 0, fmt.Errorf("replicate prediction failed: %s", prediction.Error)
	}
	if prediction.Status != "succeeded" {
		return 0, fmt.Errorf("replicate prediction did not finish in time (status: %s). Sync mode requires fast processing.", prediction.Status)
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
	config UpscaleConfig
}

type runpodJobRequest struct {
	Input map[string]interface{} `json:"input"`
}

type runpodJobResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	ExecutionTime int64  `json:"executionTime"` // in milliseconds
	Output        struct {
		Status string `json:"status"`
		Image  string `json:"image"`
	} `json:"output"`
	Error string `json:"error"`
}

func (u *runpodUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Upscaling via RunPod", "id", u.config.Model)
	key := u.config.APIKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}
	if key == "" {
		return 0, fmt.Errorf("runpod API key is required (set via -k or RUNPOD_API_KEY)")
	}

	var endpointID string
	var modelName string

	switch u.config.Model {
	case "real-esrgan", "":
		endpointID = RunPodRealESRGANEndpoint
		// Allow overriding endpoint ID via env but it's not exposed via CLI flag
		if envID := os.Getenv("RUNPOD_ENDPOINT_ID"); envID != "" {
			endpointID = envID
		}

		if u.config.Scale == 2 {
			modelName = "RealESRGAN_x2plus"
		} else {
			modelName = "RealESRGAN_x4plus"
		}
	default:
		return 0, fmt.Errorf("model not supported: %s", u.config.Model)
	}

	// 1. Convert to Base64
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return 0, err
	}

	reqBody := runpodJobRequest{
		Input: map[string]interface{}{
			"source_image": base64.StdEncoding.EncodeToString(buf.Bytes()),
			"model":        modelName,
			"scale":        u.config.Scale,
			"face_enhance": true,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("https://api.runpod.ai/v1/%s/runsync", endpointID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var job runpodJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return 0, err
	}

	if job.Status != "COMPLETED" {
		return 0, fmt.Errorf("runpod job %s: %s", job.Status, job.Error)
	}

	// 3. Decode base64 image from output
	if job.Output.Status != "ok" {
		return 0, fmt.Errorf("runpod worker returned error status: %s", job.Output.Status)
	}

	decoded, err := base64.StdEncoding.DecodeString(job.Output.Image)
	if err != nil {
		return 0, fmt.Errorf("failed to decode output image: %v", err)
	}

	_, err = w.Write(decoded)
	billableTime := time.Duration(job.ExecutionTime) * time.Millisecond
	return billableTime, err
}
