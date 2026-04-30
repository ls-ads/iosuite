package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// BenchmarkSchemaVersion is the version this code knows for
// deploy/benchmark.json. Independent from the deploy manifest's
// version because the two evolve at different cadences.
const BenchmarkSchemaVersion = "1"

// BenchmarkManifest is the parsed deploy/benchmark.json. Drives
// `iosuite endpoint benchmark` — the serve module declares the
// workload (input, request shape, metrics); iosuite owns the wire.
type BenchmarkManifest struct {
	SchemaVersion   string                  `json:"schema_version"`
	Tool            string                  `json:"tool"`
	Warmup          int                     `json:"warmup"`
	Measure         int                     `json:"measure"`
	InputResource   string                  `json:"input_resource"`
	RequestTemplate BenchmarkRequestTmpl    `json:"request_template"`
	Metrics         []BenchmarkMetric       `json:"metrics"`
}

// BenchmarkRequestTmpl carries the input.* fields iosuite ships to
// the worker on every benchmark POST. The `input.images` list is
// injected at run-time from the fetched InputResource — everything
// else here is pass-through.
type BenchmarkRequestTmpl struct {
	Input map[string]any `json:"input"`
}

// BenchmarkMetric describes one aggregation. Name appears in the
// output table; From is a numeric field on the worker's per-item
// response (e.g. "exec_ms"); Agg is one of mean/p50/p95/p99/max/min.
type BenchmarkMetric struct {
	Name string `json:"name"`
	From string `json:"from"`
	Agg  string `json:"agg"`
}

// FetchBenchmark downloads and validates a deploy/benchmark.json
// from the given URL. Caller resolves the URL from the registry +
// version (see registry.BenchmarkURL).
func FetchBenchmark(ctx context.Context, url string) (*BenchmarkManifest, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("benchmark manifest URL must be HTTPS, got %q", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("benchmark %s: build request: %w", url, err)
	}
	req.Header.Set("User-Agent", "iosuite-manifest-fetcher")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("benchmark %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("benchmark %s: read body: %w", url, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("benchmark %s: HTTP 404 — does the tool publish deploy/benchmark.json at this ref?", url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("benchmark %s: HTTP %d: %s", url, resp.StatusCode, truncate(string(body), 200))
	}
	return parseAndValidateBenchmark(body, url)
}

// LoadBenchmarkFile reads a benchmark manifest from disk for the
// --benchmark-manifest dev flag.
func LoadBenchmarkFile(path string) (*BenchmarkManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("benchmark %s: %w", path, err)
	}
	return parseAndValidateBenchmark(body, path)
}

// FetchInputResource downloads the base64-encoded input image the
// benchmark manifest names. The repo-relative path stays exactly as
// declared so iosuite can resolve against the same git tag as the
// manifest. Returns the raw decoded bytes ready to base64-re-encode
// into a request payload.
func FetchInputResource(ctx context.Context, baseURL, repoRelPath string) ([]byte, error) {
	// baseURL points at deploy/runpod.json (or deploy/benchmark.json);
	// strip the trailing path segments so the resource lookup happens
	// under the same git tag at the repo root. raw.githubusercontent
	// URLs are stable here: …/<owner>/<repo>/<tag>/<path-from-root>.
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "file://") {
		return nil, fmt.Errorf("base URL must be HTTPS or file://, got %q", baseURL)
	}
	// Find the last "/deploy/" segment in baseURL and replace from there.
	idx := strings.LastIndex(baseURL, "/deploy/")
	if idx < 0 {
		return nil, fmt.Errorf("base URL %q does not contain /deploy/ — can't resolve input_resource %q against it", baseURL, repoRelPath)
	}
	resURL := baseURL[:idx+1] + repoRelPath

	if strings.HasPrefix(resURL, "file://") {
		path := strings.TrimPrefix(resURL, "file://")
		return os.ReadFile(path)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resURL, nil)
	if err != nil {
		return nil, fmt.Errorf("input resource %s: build request: %w", resURL, err)
	}
	req.Header.Set("User-Agent", "iosuite-manifest-fetcher")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("input resource %s: %w", resURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("input resource %s: read: %w", resURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("input resource %s: HTTP %d", resURL, resp.StatusCode)
	}
	return body, nil
}

func parseAndValidateBenchmark(body []byte, source string) (*BenchmarkManifest, error) {
	var m BenchmarkManifest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("benchmark %s: parse JSON: %w", source, err)
	}
	if err := m.validate(source); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *BenchmarkManifest) validate(source string) error {
	if m.SchemaVersion != BenchmarkSchemaVersion {
		return fmt.Errorf("benchmark %s: schema_version is %q, this iosuite knows %q",
			source, m.SchemaVersion, BenchmarkSchemaVersion)
	}
	if m.Tool == "" {
		return fmt.Errorf("benchmark %s: missing required field `tool`", source)
	}
	if m.Warmup < 0 {
		return fmt.Errorf("benchmark %s: warmup must be >= 0, got %d", source, m.Warmup)
	}
	if m.Measure <= 0 {
		return fmt.Errorf("benchmark %s: measure must be > 0, got %d", source, m.Measure)
	}
	if m.InputResource == "" {
		return fmt.Errorf("benchmark %s: missing required field `input_resource`", source)
	}
	if m.RequestTemplate.Input == nil {
		return fmt.Errorf("benchmark %s: missing required field `request_template.input`", source)
	}
	if len(m.Metrics) == 0 {
		return fmt.Errorf("benchmark %s: metrics must declare at least one entry", source)
	}
	allowedAggs := map[string]bool{"mean": true, "p50": true, "p95": true, "p99": true, "max": true, "min": true}
	for i, mt := range m.Metrics {
		if mt.Name == "" || mt.From == "" {
			return fmt.Errorf("benchmark %s: metrics[%d] needs name + from", source, i)
		}
		if !allowedAggs[mt.Agg] {
			return fmt.Errorf("benchmark %s: metrics[%d].agg %q not in {mean,p50,p95,p99,max,min}", source, i, mt.Agg)
		}
	}
	return nil
}
