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
	"strings"
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
	// Opinionated names for auto-provisioning
	RunPodIOImgEndpointName = "ioimg-real-esrgan"
)

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
	Scale          int                      // e.g., 2, 4
	StatusCallback func(RunPodStatusUpdate) // Optional callback for progress updates
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
		Status      string `json:"status"` // Optional
		ImageBase64 string `json:"image_base64"`
	} `json:"output"`
	Error string `json:"error"`
}

func (u *runpodUpscaler) ensureRunPodEndpoint(ctx context.Context, key string) (string, error) {
	// 1. Check if endpoint exists via REST API
	listURL := "https://rest.runpod.io/v1/endpoints"
	listReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create list endpoints request: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	listResp, err := client.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("failed to list RunPod endpoints: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == http.StatusOK {
		var endpoints []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&endpoints); err == nil {
			for _, e := range endpoints {
				if strings.HasPrefix(e.Name, RunPodIOImgEndpointName) {
					Debug("Using existing RunPod endpoint", "id", e.ID, "matched_name", e.Name)
					return e.ID, nil
				}
			}
		}
	} else {
		body, _ := io.ReadAll(listResp.Body)
		Debug("Failed to list RunPod endpoints", "status", listResp.StatusCode, "body", string(body))
	}

	Debug("RunPod endpoint not found, creating", "name", RunPodIOImgEndpointName)

	createURL := "https://rest.runpod.io/v1/endpoints"
	reqBody := map[string]interface{}{
		"name":        RunPodIOImgEndpointName,
		"templateId":  "047z8w5i69",
		"gpuTypeIds":  []string{"NVIDIA RTX A4000"}, // 16GB tier
		"workersMin":  0,
		"workersMax":  1,
		"idleTimeout": 5,
		"flashboot":   true,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal create endpoint request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request for RunPod endpoint creation: %v", err)
	}
	// RunPod REST API uses Bearer authentication
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform RunPod endpoint creation request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("RunPod API returned status %d when creating endpoint: %s", resp.StatusCode, string(body))
	}

	var createData struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createData); err != nil {
		return "", fmt.Errorf("failed to parse RunPod endpoint creation response: %v", err)
	}

	Debug("Created new RunPod endpoint", "id", createData.ID, "name", createData.Name)
	return createData.ID, nil
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
	if key == "" {
		return 0, fmt.Errorf("runpod API key is required (set via -k or RUNPOD_API_KEY)")
	}

	u.emitStatus("infrastructure", "Connecting to RunPod...", 0)

	endpointID, err := u.ensureRunPodEndpoint(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure runpod infrastructure: %v", err)
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

	reqBody := runpodJobRequest{
		Input: map[string]interface{}{
			"image_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	// 2. Submit async job via /run
	u.emitStatus("queued", "Submitting job...", 0)
	runURL := fmt.Sprintf("https://api.runpod.ai/v2/%s/run", endpointID)
	req, err := http.NewRequestWithContext(ctx, "POST", runURL, bytes.NewBuffer(jsonData))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read runpod response: %v", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("runpod job submission failed with status %s: %s", resp.Status, string(body))
	}

	var runResp runpodJobResponse
	if err := json.Unmarshal(body, &runResp); err != nil {
		return 0, fmt.Errorf("failed to unmarshal runpod run response: %v, body: %s", err, string(body))
	}

	if runResp.ID == "" {
		return 0, fmt.Errorf("runpod returned empty job ID")
	}

	// 3. Poll /status/{jobId} until COMPLETED or FAILED
	const (
		pollInterval = 3 * time.Second
		maxWait      = 5 * time.Minute
	)
	statusURL := fmt.Sprintf("https://api.runpod.ai/v2/%s/status/%s", endpointID, runResp.ID)
	pollStart := time.Now()

	u.emitStatus("queued", "Waiting for GPU worker...", 0)

	var job runpodJobResponse
	for {
		elapsed := time.Since(pollStart)
		if elapsed > maxWait {
			return 0, fmt.Errorf("runpod job %s timed out after %s (last status: %s)", runResp.ID, maxWait, job.Status)
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(pollInterval):
		}

		statusReq, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return 0, err
		}
		statusReq.Header.Set("Authorization", "Bearer "+key)

		statusResp, err := client.Do(statusReq)
		if err != nil {
			Debug("poll error, retrying", "error", err)
			continue
		}

		statusBody, err := io.ReadAll(statusResp.Body)
		statusResp.Body.Close()
		if err != nil {
			Debug("poll read error, retrying", "error", err)
			continue
		}

		if statusResp.StatusCode != http.StatusOK {
			Debug("poll non-200, retrying", "status", statusResp.StatusCode)
			continue
		}

		if err := json.Unmarshal(statusBody, &job); err != nil {
			Debug("poll unmarshal error, retrying", "error", err)
			continue
		}

		switch job.Status {
		case "COMPLETED":
			u.emitStatus("completed", "Processing complete", elapsed)
		case "FAILED":
			return 0, fmt.Errorf("runpod job failed: %s", job.Error)
		case "IN_PROGRESS":
			u.emitStatus("in_progress", "Processing on GPU...", elapsed)
			continue
		case "IN_QUEUE":
			u.emitStatus("queued", "Waiting for GPU worker (cold start)...", elapsed)
			continue
		default:
			u.emitStatus("queued", fmt.Sprintf("Status: %s", job.Status), elapsed)
			continue
		}

		// If we get here, status is COMPLETED
		break
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
