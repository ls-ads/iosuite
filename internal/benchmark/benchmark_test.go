package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"iosuite.io/internal/manifest"
)

func bench() *manifest.BenchmarkManifest {
	return &manifest.BenchmarkManifest{
		SchemaVersion: "1",
		Tool:          "real-esrgan",
		Warmup:        2,
		Measure:       5,
		InputResource: "deploy/bench/64x64.png.b64",
		RequestTemplate: manifest.BenchmarkRequestTmpl{
			Input: map[string]any{
				"tile":           true,
				"output_format":  "jpg",
				"discard_output": true,
			},
		},
		Metrics: []manifest.BenchmarkMetric{
			{Name: "p50_ms", From: "exec_ms", Agg: "p50"},
			{Name: "p95_ms", From: "exec_ms", Agg: "p95"},
			{Name: "mean_ms", From: "exec_ms", Agg: "mean"},
		},
	}
}

func TestRun_HappyPath(t *testing.T) {
	// Mock RunPod: returns COMPLETED + a synthetic exec_ms that
	// increases per call so we can verify aggregation across
	// requests, not just the last one's value.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		// exec_ms = 100, 200, 300, … per call. With warmup=2 + measure=5,
		// the measure window sees calls 3..7 → exec_ms 300..700.
		body := fmt.Sprintf(`{"status":"COMPLETED","output":{"outputs":[{"exec_ms":%d}]}}`, int(n)*100)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// Point the benchmark at the test server. Run hardcodes the
	// api.runpod.ai URL pattern — for the test we substitute via
	// the `api.runpod.ai/v2/<id>/runsync` shape by giving the
	// endpoint id as the test server's URL minus prefix. Cleanest
	// is to extract Run's URL building behind a var.
	prevBase := runpodBaseForTesting
	runpodBaseForTesting = srv.URL
	defer func() { runpodBaseForTesting = prevBase }()

	results, err := Run(context.Background(), "test-endpoint-id", "test-key", bench(), []byte("fake-png"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := map[string]float64{
		"p50_ms":  500.0, // sorted[ceil(0.5*5)-1] = sorted[2] = 500
		"p95_ms":  700.0, // sorted[ceil(0.95*5)-1] = sorted[4] = 700
		"mean_ms": 500.0, // (300+400+500+600+700)/5
	}
	if len(results) != len(want) {
		t.Fatalf("results len = %d, want %d", len(results), len(want))
	}
	for _, r := range results {
		if got, ok := want[r.Name]; !ok {
			t.Errorf("unexpected metric %q", r.Name)
		} else if r.Value != got {
			t.Errorf("%s = %v, want %v", r.Name, r.Value, got)
		}
	}

	// Verify the wire shape we sent: warmup (2) + measure (5) = 7 calls.
	if got := atomic.LoadInt32(&calls); got != 7 {
		t.Errorf("call count = %d, want 7 (warmup+measure)", got)
	}
}

func TestRun_RejectsNonCompletedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"FAILED","output":{"error":"worker died"}}`))
	}))
	defer srv.Close()
	prevBase := runpodBaseForTesting
	runpodBaseForTesting = srv.URL
	defer func() { runpodBaseForTesting = prevBase }()

	_, err := Run(context.Background(), "id", "key", bench(), []byte("fake"))
	if err == nil {
		t.Fatal("expected error on non-COMPLETED status")
	}
	if !strings.Contains(err.Error(), "FAILED") {
		t.Errorf("error should mention status: %v", err)
	}
}

func TestBuildRequestBody_InjectsImages(t *testing.T) {
	body, err := buildRequestBody(bench(), []byte("hello-world"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	in, _ := payload["input"].(map[string]any)
	images, _ := in["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images len = %d, want 1", len(images))
	}
	first, _ := images[0].(map[string]any)
	b64, _ := first["image_base64"].(string)
	if b64 != "aGVsbG8td29ybGQ=" { // base64("hello-world")
		t.Errorf("image_base64 = %q, want base64('hello-world')", b64)
	}
	// And template fields are preserved.
	if in["tile"] != true || in["output_format"] != "jpg" {
		t.Errorf("template fields not preserved: %v", in)
	}
}

func TestPercentile_NearestRank(t *testing.T) {
	xs := []float64{300, 400, 500, 600, 700}
	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 500},
		{0.95, 700},
		{0.99, 700},
	}
	for _, tc := range cases {
		if got := percentile(xs, tc.p); got != tc.want {
			t.Errorf("percentile(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

func TestAggregate_KnownAggs(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	cases := map[string]float64{
		"mean": 3,
		"min":  1,
		"max":  5,
		"p50":  3,
	}
	for agg, want := range cases {
		if got := aggregate(xs, agg); got != want {
			t.Errorf("aggregate(%v, %q) = %v, want %v", xs, agg, got, want)
		}
	}
}

func TestFormatResults_AlignsColumns(t *testing.T) {
	out := FormatResults([]Result{
		{Name: "p50_ms", Agg: "p50", Value: 123.4},
		{Name: "p99_latency_ms", Agg: "p99", Value: 999.9},
	})
	// The longer name's value column should align with the shorter's.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	// Both lines should have "= " at the same offset.
	idx0 := strings.Index(lines[0], "= ")
	idx1 := strings.Index(lines[1], "= ")
	if idx0 != idx1 {
		t.Errorf("'= ' offset mismatch: %d vs %d in %q", idx0, idx1, out)
	}
}
