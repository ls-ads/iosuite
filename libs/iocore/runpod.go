package iocore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RunPodEndpointConfig holds configuration for auto-provisioning a RunPod Serverless Endpoint.
type RunPodEndpointConfig struct {
	Name        string   `json:"name"`
	TemplateID  string   `json:"templateId"`
	GPUTypeIDs  []string `json:"gpuTypeIds"`
	WorkersMin  int      `json:"workersMin"`
	WorkersMax  int      `json:"workersMax"`
	IdleTimeout int      `json:"idleTimeout"`
	Flashboot   bool     `json:"flashboot"`
}

// EnsureRunPodEndpoint checks if a RunPod endpoint with the given name prefix exists.
// If it does, it returns the endpoint ID. Otherwise, it creates a new endpoint
// using the provided config and returns its ID.
func EnsureRunPodEndpoint(ctx context.Context, key string, config RunPodEndpointConfig) (string, error) {
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
				if strings.HasPrefix(e.Name, config.Name) {
					Debug("Using existing RunPod endpoint", "id", e.ID, "matched_name", e.Name)
					return e.ID, nil
				}
			}
		}
	} else {
		body, _ := io.ReadAll(listResp.Body)
		Debug("Failed to list RunPod endpoints", "status", listResp.StatusCode, "body", string(body))
	}

	Debug("RunPod endpoint not found, creating", "name", config.Name)

	createURL := "https://rest.runpod.io/v1/endpoints"
	jsonData, err := json.Marshal(config)
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

// RunPodJobRequest represents the input for a RunPod serverless job.
type RunPodJobRequest struct {
	Input map[string]interface{} `json:"input"`
}

// RunPodJobResponse represents the response from a RunPod serverless job.
type RunPodJobResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	ExecutionTime int64  `json:"executionTime"` // in milliseconds
	Output        struct {
		Status      string `json:"status"` // Optional
		ImageBase64 string `json:"image_base64"`
	} `json:"output"`
	Error string `json:"error"`
}

// SubmitRunPodJob submits an async job to a RunPod endpoint.
func SubmitRunPodJob(ctx context.Context, key, endpointID string, input map[string]interface{}) (string, error) {
	reqBody := RunPodJobRequest{
		Input: input,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	runURL := fmt.Sprintf("https://api.runpod.ai/v2/%s/run", endpointID)
	req, err := http.NewRequestWithContext(ctx, "POST", runURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read runpod response: %v", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("runpod job submission failed with status %s: %s", resp.Status, string(body))
	}

	var runResp RunPodJobResponse
	if err := json.Unmarshal(body, &runResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal runpod run response: %v, body: %s", err, string(body))
	}

	if runResp.ID == "" {
		return "", fmt.Errorf("runpod returned empty job ID")
	}

	return runResp.ID, nil
}

// PollRunPodJob polls a RunPod job until it completes or fails.
func PollRunPodJob(ctx context.Context, key, endpointID, jobID string, statusCallback func(phase, message string, elapsed time.Duration)) (*RunPodJobResponse, error) {
	const (
		pollInterval = 3 * time.Second
		maxWait      = 5 * time.Minute
	)
	statusURL := fmt.Sprintf("https://api.runpod.ai/v2/%s/status/%s", endpointID, jobID)
	pollStart := time.Now()

	client := &http.Client{}
	var job RunPodJobResponse

	for {
		elapsed := time.Since(pollStart)
		if elapsed > maxWait {
			return nil, fmt.Errorf("runpod job %s timed out after %s (last status: %s)", jobID, maxWait, job.Status)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		statusReq, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return nil, err
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
			if statusCallback != nil {
				statusCallback("completed", "Processing complete", elapsed)
			}
			return &job, nil
		case "FAILED":
			return nil, fmt.Errorf("runpod job failed: %s", job.Error)
		case "IN_PROGRESS":
			if statusCallback != nil {
				statusCallback("in_progress", "Processing on GPU...", elapsed)
			}
			continue
		case "IN_QUEUE":
			if statusCallback != nil {
				statusCallback("queued", "Waiting for GPU worker (cold start)...", elapsed)
			}
			continue
		default:
			if statusCallback != nil {
				statusCallback("queued", fmt.Sprintf("Status: %s", job.Status), elapsed)
			}
			continue
		}
	}
}
