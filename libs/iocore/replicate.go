package iocore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ReplicatePredictionRequest represents the input for a Replicate prediction.
type ReplicatePredictionRequest struct {
	Input map[string]interface{} `json:"input"`
}

// ReplicatePredictionResponse represents the response from a Replicate prediction.
type ReplicatePredictionResponse struct {
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

// RunReplicatePrediction starts a prediction and waits for it to finish.
func RunReplicatePrediction(ctx context.Context, key, url string, input map[string]interface{}) (*ReplicatePredictionResponse, error) {
	reqBody := ReplicatePredictionRequest{
		Input: input,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+key)
	req.Header.Set("Content-Type", "application/json")
	// "Prefer: wait" tells Replicate to wait up to a certain amount of time before returning
	req.Header.Set("Prefer", "wait")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("replicate creation failed: %s, body: %s", resp.Status, string(body))
	}

	var prediction ReplicatePredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&prediction); err != nil {
		return nil, err
	}

	if prediction.Status == "failed" {
		return nil, fmt.Errorf("replicate prediction failed: %s", prediction.Error)
	}
	if prediction.Status != "succeeded" {
		return nil, fmt.Errorf("replicate prediction did not finish in time (status: %s). Sync mode requires fast processing.", prediction.Status)
	}

	return &prediction, nil
}
