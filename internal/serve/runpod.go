// RunPodProvider — `iosuite serve --provider runpod` backend.
//
// Translates the daemon's HTTP surface (multipart `image` field upload)
// to RunPod's serverless API shape (JSON with base64-encoded images),
// posts to /runsync, falls back to polling /status when /runsync hits
// its 90 s server-side timeout, and returns the upscaled bytes.
//
// Same flow iosuite.io's backend uses today via direct RunPod calls;
// folding it behind the Provider interface lets self-hosters point a
// LOCAL `iosuite serve` at a CLOUD RunPod endpoint without any client
// code change. iosuite.io can do the same swap in Phase 4 by pointing
// its existing client at a `iosuite serve --provider runpod` daemon.
package serve

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
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

	// SyncTimeout is how long /runsync waits before falling back to
	// /run + /status polling. RunPod's server-side cap is 90 s; we
	// match. Caller may bump for batch jobs; default is 120 to give
	// our own client a few seconds beyond RunPod's timeout to read
	// the still-running response and switch to polling cleanly.
	SyncTimeout time.Duration

	// PollMax is the longest we wait via /status polling before
	// giving up. Should be larger than typical cold-start +
	// inference time for the GPU class on the endpoint. Default
	// 300 s matches iosuite.io's backend.
	PollMax time.Duration

	// OutputFormat — "jpg" or "png". Defaults to "jpg" because the
	// resulting payloads are 5-10× smaller than PNG and the typical
	// caller is downstream resizing anyway.
	OutputFormat string

	// Tile, when true, sends `tile: true` in the RunPod request so
	// inputs > 1280² get the slice/blend path. Safe to leave on
	// always — sub-1280² inputs short-circuit through the worker.
	Tile bool
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
	if opts.OutputFormat == "" {
		opts.OutputFormat = "jpg"
	}
	return &RunPodProvider{
		opts: opts,
		http: &http.Client{Timeout: opts.SyncTimeout + 30*time.Second},
	}
}

// Start validates configuration and confirms the endpoint exists by
// hitting RunPod's per-endpoint /health. The probe is cheap and
// catches misconfigured endpoint IDs before we accept user traffic
// (otherwise the first inbound request would 502 with a confusing
// runpod-side message).
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

// Upscale extracts the multipart `image` field, base64-encodes it,
// posts to RunPod, and returns the upscaled bytes + content-type.
// /runsync first; /run + /status polling fallback if the queue is
// cold. Mirrors iosuite.io's backend submit_upscale_batch one-to-one.
func (r *RunPodProvider) Upscale(ctx context.Context, body io.Reader, contentType string) ([]byte, string, error) {
	imgBytes, err := extractImageField(body, contentType)
	if err != nil {
		// Caller-side decode error — return as a regular error so
		// the HTTP layer maps to 500 (a 400 would also work; the
		// `image` field being absent is genuinely the caller's
		// fault, not the provider's).
		return nil, "", err
	}

	payload := map[string]any{
		"input": map[string]any{
			"images":          []map[string]any{{"image_base64": base64.StdEncoding.EncodeToString(imgBytes)}},
			"output_format":   r.opts.OutputFormat,
			"tile":            r.opts.Tile,
			"discard_output":  false,
		},
	}
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	syncURL := fmt.Sprintf("%s/%s/runsync", runpodBase, r.opts.EndpointID)
	resp, err := r.post(ctx, syncURL, bodyJSON)
	if err != nil {
		return nil, "", AsProviderError(err)
	}
	if status, _ := resp["status"].(string); status == "IN_QUEUE" || status == "IN_PROGRESS" {
		jobID, _ := resp["id"].(string)
		if jobID == "" {
			return nil, "", AsProviderError(fmt.Errorf("runpod %s but no job id in response", status))
		}
		resp, err = r.pollUntilDone(ctx, jobID)
		if err != nil {
			return nil, "", AsProviderError(err)
		}
	}
	return decodeRunPodResponse(resp, r.opts.OutputFormat)
}

func (r *RunPodProvider) Close() error { return nil }

// truncate caps a string for inclusion in error messages — keeps
// runpod's HTML 502 pages from ending up in the daemon's logs at
// full length when the upstream is misbehaving.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// post sends a JSON body to RunPod and returns the parsed response
// envelope. Splits the auth + JSON-roundtrip out so /runsync and
// /status share the same error handling.
func (r *RunPodProvider) post(ctx context.Context, url string, body []byte) (map[string]any, error) {
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
	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("runpod %s: parse: %w", url, err)
	}
	return out, nil
}

func (r *RunPodProvider) pollUntilDone(ctx context.Context, jobID string) (map[string]any, error) {
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
		var out map[string]any
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, fmt.Errorf("runpod /status: parse: %w", err)
		}
		switch out["status"] {
		case "COMPLETED":
			return out, nil
		case "FAILED", "CANCELLED", "TIMED_OUT":
			return nil, fmt.Errorf("runpod job %s: %v", jobID, out["status"])
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

// extractImageField walks the multipart body and returns the bytes of
// the first part named `image` (the contract real-esrgan-serve serve
// also follows). Caller passes the original Content-Type header so
// we can pull the boundary parameter out of it.
func extractImageField(body io.Reader, contentType string) ([]byte, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("parse content-type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("multipart Content-Type missing boundary")
	}
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, errors.New(`multipart body has no "image" field`)
		}
		if err != nil {
			return nil, fmt.Errorf("read multipart: %w", err)
		}
		if part.FormName() == "image" {
			data, err := io.ReadAll(part)
			part.Close()
			if err != nil {
				return nil, fmt.Errorf("read image part: %w", err)
			}
			return data, nil
		}
		part.Close()
	}
}

// decodeRunPodResponse pulls the upscaled bytes out of a RunPod
// COMPLETED envelope. The worker returns:
//
//	{"output": {"outputs": [{"image_base64": "...", ...}], "_diagnostics": {...}}}
//
// We grab the first output's image_base64 and decode it. If
// _diagnostics.per_item_errors is non-empty, the worker rejected the
// input — surface that as a provider error.
func decodeRunPodResponse(resp map[string]any, outputFormat string) ([]byte, string, error) {
	output, ok := resp["output"].(map[string]any)
	if !ok {
		return nil, "", AsProviderError(fmt.Errorf("runpod response missing `output` field"))
	}
	if diag, ok := output["_diagnostics"].(map[string]any); ok {
		if errs, _ := diag["per_item_errors"].([]any); len(errs) > 0 {
			first, _ := errs[0].(map[string]any)
			msg, _ := first["error"].(string)
			return nil, "", AsProviderError(fmt.Errorf("worker error: %s", msg))
		}
	}
	outputs, ok := output["outputs"].([]any)
	if !ok || len(outputs) == 0 {
		return nil, "", AsProviderError(fmt.Errorf("runpod response missing `outputs[]`"))
	}
	first, ok := outputs[0].(map[string]any)
	if !ok {
		return nil, "", AsProviderError(fmt.Errorf("runpod outputs[0] is not an object"))
	}
	b64, _ := first["image_base64"].(string)
	if b64 == "" {
		return nil, "", AsProviderError(fmt.Errorf("runpod outputs[0] missing image_base64"))
	}
	imgBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", AsProviderError(fmt.Errorf("decode image_base64: %w", err))
	}
	contentType := "image/jpeg"
	if outputFormat == "png" {
		contentType = "image/png"
	}
	return imgBytes, contentType, nil
}
