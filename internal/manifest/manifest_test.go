package manifest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `{
  "schema_version": "1",
  "tool": "real-esrgan",
  "image": "ghcr.io/ls-ads/real-esrgan-serve:runpod-trt-0.2.1",
  "endpoint": {
    "container_disk_gb": 10,
    "workers_max_default": 2,
    "idle_timeout_s_default": 30,
    "flashboot_default": true,
    "min_cuda_version": "12.8"
  },
  "gpu_pools": {"rtx-4090": "ADA_24"},
  "env": []
}`

func TestFetch_Happy(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validManifest))
	}))
	defer srv.Close()

	// httptest TLS uses a self-signed cert; swap the default
	// transport on a copy so the test client trusts it.
	httpClientWithSelfSigned(t, srv)

	m, err := Fetch(context.Background(), srv.URL+"/deploy/runpod.json")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if m.Tool != "real-esrgan" {
		t.Errorf("Tool = %q, want real-esrgan", m.Tool)
	}
	if m.Endpoint.MinCudaVersion != "12.8" {
		t.Errorf("MinCudaVersion = %q, want 12.8", m.Endpoint.MinCudaVersion)
	}
	if m.GPUPools["rtx-4090"] != "ADA_24" {
		t.Errorf("GPUPools[rtx-4090] = %q, want ADA_24", m.GPUPools["rtx-4090"])
	}
}

func TestFetch_Rejects404(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	httpClientWithSelfSigned(t, srv)

	_, err := Fetch(context.Background(), srv.URL+"/deploy/runpod.json")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404: %v", err)
	}
}

func TestFetch_RejectsHTTP(t *testing.T) {
	_, err := Fetch(context.Background(), "http://example.com/deploy/runpod.json")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("expected HTTPS-required error, got %v", err)
	}
}

func TestLoadFile_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runpod.json")
	if err := os.WriteFile(path, []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if m.Image != "ghcr.io/ls-ads/real-esrgan-serve:runpod-trt-0.2.1" {
		t.Errorf("unexpected image: %q", m.Image)
	}
}

func TestValidate_RejectsBadSchema(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // substring
	}{
		{"wrong schema_version", strings.Replace(validManifest, `"schema_version": "1"`, `"schema_version": "999"`, 1), "schema_version"},
		{"missing tool", strings.Replace(validManifest, `"tool": "real-esrgan",`, ``, 1), "tool"},
		{"image without tag", strings.Replace(validManifest, `:runpod-trt-0.2.1`, ``, 1), "no tag"},
		{"zero disk", strings.Replace(validManifest, `"container_disk_gb": 10`, `"container_disk_gb": 0`, 1), "container_disk_gb"},
		{"empty gpu_pools", strings.Replace(validManifest, `"gpu_pools": {"rtx-4090": "ADA_24"}`, `"gpu_pools": {}`, 1), "gpu_pools"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "m.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q: %v", tc.want, err)
			}
		})
	}
}

func TestValidate_RejectsUnknownField(t *testing.T) {
	bad := strings.Replace(validManifest, `"env": []`, `"env": [], "garbage": true`, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error on unknown field")
	}
	if !strings.Contains(err.Error(), "garbage") && !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error should reference the bad field: %v", err)
	}
}

// httpClientWithSelfSigned points the package's HTTPClient at one
// that trusts the httptest server's self-signed cert for the test's
// lifetime. Cleaner than rewiring http.DefaultTransport since Fetch
// uses HTTPClient by name.
func httpClientWithSelfSigned(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := HTTPClient
	HTTPClient = srv.Client()
	t.Cleanup(func() { HTTPClient = prev })
}
