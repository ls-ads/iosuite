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
	RunPodIOImgEndpointName = "ioimg-upscale-real-esrgan"
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
		Status string `json:"status"` // Optional
		Image  string `json:"image"`
	} `json:"output"`
	Error string `json:"error"`
}

type runpodGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type runpodGraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (u *runpodUpscaler) ensureRunPodEndpoint(ctx context.Context, key string) (string, error) {
	// 1. Check if endpoint exists
	listEndpointsQuery := `query { myself { endpoints { id name } } }`
	respBody, err := u.runGraphQL(ctx, key, listEndpointsQuery, nil)
	if err != nil {
		return "", err
	}

	var endpointsData struct {
		Myself struct {
			Endpoints []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"endpoints"`
		} `json:"myself"`
	}

	if err := json.Unmarshal(respBody, &endpointsData); err == nil {
		for _, e := range endpointsData.Myself.Endpoints {
			if strings.HasPrefix(e.Name, RunPodIOImgEndpointName) {
				Info("Using existing RunPod endpoint", "id", e.ID, "matched_name", e.Name)
				return e.ID, nil
			}
		}
	}

	return "", fmt.Errorf("RunPod endpoint '%s' not found. Please create a serverless endpoint with this name in the RunPod Dashboard.", RunPodIOImgEndpointName)
}

func (u *runpodUpscaler) runGraphQL(ctx context.Context, key, query string, vars map[string]interface{}) (json.RawMessage, error) {
	reqBody := runpodGraphQLRequest{
		Query:     query,
		Variables: vars,
	}
	jsonData, _ := json.Marshal(reqBody)
	url := "https://api.runpod.io/graphql?api_key=" + key
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var gqlResp runpodGraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("runpod graphql error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data, nil
}

func (u *runpodUpscaler) Upscale(ctx context.Context, r io.Reader, w io.Writer) (time.Duration, error) {
	Info("Preparing RunPod infrastructure", "model", u.config.Model)
	key := u.config.APIKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}
	if key == "" {
		return 0, fmt.Errorf("runpod API key is required (set via -k or RUNPOD_API_KEY)")
	}

	endpointID, err := u.ensureRunPodEndpoint(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure runpod infrastructure: %v", err)
	}

	var modelName string
	switch u.config.Model {
	case "real-esrgan", "":
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
	Info("Sending RunPod request", "payload_prefix", string(jsonData)[:100])

	url := fmt.Sprintf("https://api.runpod.ai/v2/%s/runsync?wait=90000", endpointID)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read runpod response: %v", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("runpod job failed with status %s: %s", resp.Status, string(body))
	}

	var job runpodJobResponse
	if err := json.Unmarshal(body, &job); err != nil {
		return 0, fmt.Errorf("failed to unmarshal runpod response: %v, body: %s", err, string(body))
	}

	if job.Status != "COMPLETED" {
		return 0, fmt.Errorf("runpod job %s: %s", job.Status, job.Error)
	}

	// 3. Decode base64 image from output
	if job.Output.Image == "" {
		return 0, fmt.Errorf("runpod worker returned no image in output (status: %s)", job.Output.Status)
	}

	decoded, err := base64.StdEncoding.DecodeString(job.Output.Image)
	if err != nil {
		return 0, fmt.Errorf("failed to decode output image: %v", err)
	}

	_, err = w.Write(decoded)
	billableTime := time.Duration(job.ExecutionTime) * time.Millisecond
	return billableTime, err
}
