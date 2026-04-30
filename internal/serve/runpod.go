// RunPodProvider — `iosuite serve --provider runpod` backend.
//
// Forwards the raw request body to RunPod's serverless API
// (/runsync, falling back to /run + /status polling on cold start)
// and returns the raw response body. The wire shape between iosuite
// serve and RunPod is identical, so the provider is mostly transport —
// add auth, post, parse the envelope's `status` to decide whether to
// poll, return.
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
	// 10 m mirrors the iosuite.io client cap.
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
		// 10 minutes. Covers a cold-start (~45 s) plus a tile-mode 4K
		// upscale (~90 s on rtx-4090) plus enough buffer for queue
		// time when other tenants are warming the same host pool.
		// iosuite.io's pre-Phase-4 client cap was 7.3 min; we sit
		// comfortably above it so the bottleneck is RunPod, not us.
		opts.PollMax = 10 * time.Minute
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

// Run forwards the raw request body to RunPod /runsync and returns
// the raw response body. Pass-through; iosuite doesn't interpret
// the inner contents. Status polling for queued/in-progress jobs
// happens internally so the caller sees a single round-trip.
func (r *RunPodProvider) Run(ctx context.Context, requestBody []byte) ([]byte, error) {
	start := time.Now()
	syncURL := fmt.Sprintf("%s/%s/runsync", runpodBase, r.opts.EndpointID)
	logf("runpod.runsync.post bytes=%d", len(requestBody))

	respBody, err := r.post(ctx, syncURL, requestBody)
	if err != nil {
		logf("runpod.runsync.err err=%q dur=%s", err.Error(), time.Since(start))
		return nil, AsProviderError(err)
	}

	// Peek at the envelope to decide whether to poll. We only need
	// `status` and `id` — everything else passes through unchanged.
	status, jobID := peekStatusAndID(respBody)
	logf("runpod.runsync.resp status=%s id=%s dur=%s", status, jobID, time.Since(start))

	if status == "IN_QUEUE" || status == "IN_PROGRESS" {
		if jobID == "" {
			return nil, AsProviderError(fmt.Errorf("runpod %s but no job id in response", status))
		}
		respBody, err = r.pollUntilDone(ctx, jobID)
		if err != nil {
			logf("runpod.poll.err job=%s err=%q dur=%s", jobID, err.Error(), time.Since(start))
			return nil, AsProviderError(err)
		}
		status, _ = peekStatusAndID(respBody)
		logf("runpod.poll.done job=%s status=%s dur=%s", jobID, status, time.Since(start))
	}

	if status != "COMPLETED" {
		return nil, AsProviderError(fmt.Errorf("runpod terminal status: %s", status))
	}
	return respBody, nil
}

// Close — RunPodProvider holds no long-lived resources beyond the
// http.Client (which is GC'd).
func (r *RunPodProvider) Close() error { return nil }

// peekStatusAndID extracts just the two envelope fields we care
// about, leaving the rest of the bytes untouched. Avoids
// round-tripping the (potentially large) output payload through a
// typed Go struct + re-marshal.
func peekStatusAndID(body []byte) (status, id string) {
	var env struct {
		Status string `json:"status"`
		ID     string `json:"id,omitempty"`
	}
	_ = json.Unmarshal(body, &env)
	return env.Status, env.ID
}

func (r *RunPodProvider) post(ctx context.Context, url string, body []byte) ([]byte, error) {
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
	return respBody, nil
}

func (r *RunPodProvider) pollUntilDone(ctx context.Context, jobID string) ([]byte, error) {
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
		status, _ := peekStatusAndID(respBody)
		switch status {
		case "COMPLETED":
			return respBody, nil
		case "FAILED", "CANCELLED", "TIMED_OUT":
			return nil, fmt.Errorf("runpod job %s: %s", jobID, status)
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
