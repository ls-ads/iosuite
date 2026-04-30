// Package benchmark runs the workload declared in a *-serve module's
// deploy/benchmark.json against a deployed serverless endpoint.
//
// Split (per the iosuite/serve-module architecture):
//   - The serve module owns the workload: which input image to use,
//     how many warmup vs measurement requests, what numeric fields
//     to aggregate, what aggregations matter. All declared in
//     deploy/benchmark.json.
//   - iosuite owns the wire: HTTP POST loop, timing, percentile math,
//     output formatting.
//
// Today this is RunPod-only, hitting api.runpod.ai/v2/<id>/runsync
// with the manifest's request_template + the manifest-named input
// image base64-injected into input.images[]. Future providers slot
// in behind the same Runner interface.
package benchmark

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"iosuite.io/internal/manifest"
)

// runpodBaseForTesting points at api.runpod.ai for prod and a
// httptest server URL during tests. Package-level var so tests
// can swap without threading the URL through every callsite.
var runpodBaseForTesting = "https://api.runpod.ai"

// Result is the per-metric output of one run.
type Result struct {
	Name  string  // metric name from the benchmark manifest
	Agg   string  // mean | p50 | p95 | p99 | max | min
	Value float64 // computed aggregate
}

// Run executes warmup + measure POSTs against the given RunPod
// endpoint and returns one Result per declared metric.
//
// inputBytes is the raw bytes of the input resource named in
// manifest.InputResource (the caller fetched it).
func Run(
	ctx context.Context,
	endpointID, apiKey string,
	man *manifest.BenchmarkManifest,
	inputBytes []byte,
) ([]Result, error) {
	if endpointID == "" {
		return nil, fmt.Errorf("endpoint id required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("RunPod API key required")
	}

	// Build the request body from the manifest's request_template,
	// injecting the input.images list from inputBytes (base64-encoded).
	// Operators sometimes look at this in TRACE — keep it readable.
	body, err := buildRequestBody(man, inputBytes)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/v2/%s/runsync", runpodBaseForTesting, endpointID)
	httpClient := &http.Client{Timeout: 12 * time.Minute}

	post := func() (map[string]any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 300))
		}
		var env map[string]any
		if err := json.Unmarshal(respBody, &env); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		status, _ := env["status"].(string)
		if status != "COMPLETED" {
			return nil, fmt.Errorf("status=%s (want COMPLETED): %s", status, truncate(string(respBody), 300))
		}
		return env, nil
	}

	// Warmup. We discard results; only purpose is to absorb cold-start
	// variance so the measurement window reflects warm-pool perf.
	for i := 0; i < man.Warmup; i++ {
		if _, err := post(); err != nil {
			return nil, fmt.Errorf("warmup %d/%d: %w", i+1, man.Warmup, err)
		}
	}

	// Measure. Capture each metric source field per request so we
	// can aggregate across requests at the end.
	values := make(map[string][]float64)
	for _, mt := range man.Metrics {
		values[mt.From] = make([]float64, 0, man.Measure)
	}
	for i := 0; i < man.Measure; i++ {
		env, err := post()
		if err != nil {
			return nil, fmt.Errorf("measure %d/%d: %w", i+1, man.Measure, err)
		}
		// Each metric's `from` is a numeric field on the worker's
		// per-item output. Look it up under output.outputs[0].<from>
		// (the standard real-esrgan-serve handler shape, mirrored by
		// every iosuite-serve compatible worker).
		out, _ := env["output"].(map[string]any)
		outs, _ := out["outputs"].([]any)
		var first map[string]any
		if len(outs) > 0 {
			first, _ = outs[0].(map[string]any)
		}
		for _, mt := range man.Metrics {
			if first == nil {
				return nil, fmt.Errorf("measure %d/%d: response had no output.outputs[0] to read %q from", i+1, man.Measure, mt.From)
			}
			v, ok := numericField(first, mt.From)
			if !ok {
				return nil, fmt.Errorf("measure %d/%d: response.output.outputs[0].%s is missing or non-numeric", i+1, man.Measure, mt.From)
			}
			values[mt.From] = append(values[mt.From], v)
		}
	}

	results := make([]Result, 0, len(man.Metrics))
	for _, mt := range man.Metrics {
		results = append(results, Result{
			Name:  mt.Name,
			Agg:   mt.Agg,
			Value: aggregate(values[mt.From], mt.Agg),
		})
	}
	return results, nil
}

func buildRequestBody(man *manifest.BenchmarkManifest, inputBytes []byte) ([]byte, error) {
	// Deep-copy the request_template so we don't mutate the parsed
	// manifest if Run gets called multiple times.
	tmpl := make(map[string]any, len(man.RequestTemplate.Input))
	for k, v := range man.RequestTemplate.Input {
		tmpl[k] = v
	}
	tmpl["images"] = []map[string]any{
		{"image_base64": base64.StdEncoding.EncodeToString(inputBytes)},
	}
	return json.Marshal(map[string]any{"input": tmpl})
}

// numericField reads a numeric value from a JSON-parsed map. Handles
// the int / float ambiguity that JSON unmarshalling into any leaves —
// numbers come back as float64, but if a manifest field happens to be
// integral the test should still pass.
func numericField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// aggregate runs one of the supported aggregations on the captured
// values. Empty input returns 0 — the caller has already validated
// `measure > 0`, so this is defensive.
func aggregate(xs []float64, agg string) float64 {
	if len(xs) == 0 {
		return 0
	}
	switch agg {
	case "mean":
		var s float64
		for _, v := range xs {
			s += v
		}
		return s / float64(len(xs))
	case "min":
		m := xs[0]
		for _, v := range xs[1:] {
			if v < m {
				m = v
			}
		}
		return m
	case "max":
		m := xs[0]
		for _, v := range xs[1:] {
			if v > m {
				m = v
			}
		}
		return m
	case "p50":
		return percentile(xs, 0.50)
	case "p95":
		return percentile(xs, 0.95)
	case "p99":
		return percentile(xs, 0.99)
	}
	return 0
}

func percentile(xs []float64, p float64) float64 {
	sorted := make([]float64, len(xs))
	copy(sorted, xs)
	sort.Float64s(sorted)
	// Nearest-rank: i = ceil(p*N). Clamp to [0, N-1].
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// FormatResults renders the results as a human-readable two-column
// table. Same shape across tools so the output is grep-friendly.
func FormatResults(results []Result) string {
	if len(results) == 0 {
		return "(no metrics produced)\n"
	}
	maxName := 0
	for _, r := range results {
		if len(r.Name) > maxName {
			maxName = len(r.Name)
		}
	}
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "  %-*s  %s = %s\n",
			maxName, r.Name, r.Agg, formatValue(r.Value))
	}
	return b.String()
}

func formatValue(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}
