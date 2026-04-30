// RunPodProvider — `iosuite serve --provider runpod` backend.
//
// Forwards the daemon's JobRequest to RunPod's serverless API
// (/runsync, falling back to /run + /status polling on cold start)
// and parses the response. The wire shape on iosuite serve's side
// matches RunPod's exactly, so the provider is mostly transport —
// add auth, post, parse, return.
//
// iosuite.io's `submit_upscale_batch` already sends RunPod-shaped
// JSON; pointing it at `iosuite serve --provider runpod` is a one-line
// URL swap. Self-hosters get the same benefit when they want to
// route a local app through cloud GPU without touching client code.
package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RunPodProviderOptions configures the upstream connection.
type RunPodProviderOptions struct {
	// EndpointID is the RunPod serverless endpoint id (e.g.
	// `l03sfgxn8crha1`). Required.
	EndpointID string

	// APIKey authenticates with RunPod. Required.
	APIKey string

	// SyncTimeout — how long /runsync waits before falling back to
	// /run + /status polling. RunPod's server-side cap is 90 s; we
	// give our own client 30 s of headroom.
	SyncTimeout time.Duration

	// PollMax — longest we poll /status before giving up. Default
	// 300 s mirrors iosuite.io's backend.
	PollMax time.Duration
}

// RunPodProvider implements Provider against a RunPod endpoint.
type RunPodProvider struct {
	opts RunPodProviderOptions
	http *http.Client
}

const runpodBase = "https://api.runpod.ai/v2"

// NewRunPod returns a configured RunPodProvider. Doesn't make any
// network calls — Start does the auth probe.
func NewRunPod(opts RunPodProviderOptions) *RunPodProvider {
	if opts.SyncTimeout == 0 {
		opts.SyncTimeout = 120 * time.Second
	}
	if opts.PollMax == 0 {
		opts.PollMax = 300 * time.Second
	}
	return &RunPodProvider{
		opts: opts,
		http: &http.Client{Timeout: opts.SyncTimeout + 30*time.Second},
	}
}

// Start validates configuration and confirms the endpoint exists by
// hitting RunPod's per-endpoint /health. Catches misconfigured
// endpoint IDs before we accept user traffic — otherwise the first
// inbound request would 502 with a confusing runpod-side message.
func (r *RunPodProvider) Start(ctx context.Context) error {
	if r.opts.EndpointID == "" {
		return errors.New("RunPodProvider: EndpointID is required")
	}
	if r.opts.APIKey == "" {
		return errors.New("RunPodProvider: APIKey is required")
	}

	url := fmt.Sprintf("%s/%s/health", runpodBase, r.opts.EndpointID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+r.opts.APIKey)
	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("runpod /health probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("runpod /health: HTTP %d (check RUNPOD_API_KEY)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("runpod /health: endpoint %q not found (check --endpoint-id)", r.opts.EndpointID)
	}
	return fmt.Errorf("runpod /health: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// Run forwards the JobRequest to RunPod /runsync and returns the
// response. The request shape iosuite serve received matches what
// RunPod's worker expects, so nothing to translate.
func (r *RunPodProvider) Run(ctx context.Context, job JobRequest) (*JobResponse, error) {
	bodyJSON, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}

	syncURL := fmt.Sprintf("%s/%s/runsync", runpodBase, r.opts.EndpointID)
	resp, err := r.post(ctx, syncURL, bodyJSON)
	if err != nil {
		return nil, AsProviderError(err)
	}
	if resp.Status == "IN_QUEUE" || resp.Status == "IN_PROGRESS" {
		jobID := resp.ID
		if jobID == "" {
			return nil, AsProviderError(fmt.Errorf("runpod %s but no job id in response", resp.Status))
		}
		resp, err = r.pollUntilDone(ctx, jobID)
		if err != nil {
			return nil, AsProviderError(err)
		}
	}
	if resp.Status != "COMPLETED" {
		return nil, AsProviderError(fmt.Errorf("runpod terminal status: %s", resp.Status))
	}
	return &JobResponse{Status: "COMPLETED", Output: resp.Output}, nil
}

// Close — RunPodProvider holds no long-lived resources beyond the
// http.Client (which is GC'd).
func (r *RunPodProvider) Close() error { return nil }

// runPodEnvelope is the loosely-typed envelope RunPod returns from
// /runsync and /status. It carries a `status` field, an `id` (only on
// /runsync responses that hit IN_QUEUE), and the worker's `output`.
type runPodEnvelope struct {
	Status string    `json:"status"`
	ID     string    `json:"id,omitempty"`
	Output JobOutput `json:"output"`
}

func (r *RunPodProvider) post(ctx context.Context, url string, body []byte) (*runPodEnvelope, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runpod %s: HTTP %d: %s", url, resp.StatusCode, truncate(string(respBody), 300))
	}
	var env runPodEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("runpod %s: parse: %w", url, err)
	}
	return &env, nil
}

func (r *RunPodProvider) pollUntilDone(ctx context.Context, jobID string) (*runPodEnvelope, error) {
	statusURL := fmt.Sprintf("%s/%s/status/%s", runpodBase, r.opts.EndpointID, jobID)
	deadline := time.Now().Add(r.opts.PollMax)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		req.Header.Set("Authorization", "Bearer "+r.opts.APIKey)
		resp, err := r.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("runpod /status: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("runpod /status: HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
		}
		var env runPodEnvelope
		if err := json.Unmarshal(respBody, &env); err != nil {
			return nil, fmt.Errorf("runpod /status: parse: %w", err)
		}
		switch env.Status {
		case "COMPLETED":
			return &env, nil
		case "FAILED", "CANCELLED", "TIMED_OUT":
			return nil, fmt.Errorf("runpod job %s: %s", jobID, env.Status)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("runpod job %s still running after %s", jobID, r.opts.PollMax)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// truncate caps a string for inclusion in error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
