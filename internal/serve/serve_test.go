package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubProvider lets tests check what /runsync forwards to the
// provider without spinning up a real subprocess or RunPod call.
type stubProvider struct {
	startErr error
	runFn    func(JobRequest) (*JobResponse, error)
}

func (s *stubProvider) Start(context.Context) error { return s.startErr }
func (s *stubProvider) Run(_ context.Context, j JobRequest) (*JobResponse, error) {
	return s.runFn(j)
}
func (s *stubProvider) Close() error { return nil }

// newTestServer wires a Server-equivalent (just the mux) over a stub
// provider so we can exercise the HTTP layer without spawning a
// subprocess or hitting real RunPod.
func newTestServer(t *testing.T, p Provider) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/runsync", makeJobHandler(p))
	mux.HandleFunc("/upscale", makeJobHandler(p))
	return httptest.NewServer(mux)
}

func TestRunsync_HappyPath(t *testing.T) {
	p := &stubProvider{
		runFn: func(j JobRequest) (*JobResponse, error) {
			if len(j.Input.Images) != 1 || j.Input.Images[0].ImageBase64 != "Zm9v" {
				t.Errorf("provider got unexpected input: %+v", j)
			}
			return &JobResponse{
				Status: "COMPLETED",
				Output: JobOutput{Outputs: []ImageOutput{{ImageBase64: "YmFy", ExecMS: 42}}},
			}, nil
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	req := JobRequest{Input: JobInput{Images: []ImageInput{{ImageBase64: "Zm9v"}}}}
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(raw))
	}
	var got JobResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "COMPLETED" {
		t.Errorf("status = %q, want COMPLETED", got.Status)
	}
	if len(got.Output.Outputs) != 1 || got.Output.Outputs[0].ImageBase64 != "YmFy" {
		t.Errorf("output didn't round-trip: %+v", got)
	}
}

func TestRunsync_LegacySingleImageNormalised(t *testing.T) {
	// Callers using the old shape (image_base64 at the top level)
	// should be normalised into the array form so providers see one
	// shape only.
	p := &stubProvider{
		runFn: func(j JobRequest) (*JobResponse, error) {
			if len(j.Input.Images) != 1 || j.Input.Images[0].ImageBase64 != "Zm9v" {
				return nil, fmt.Errorf("legacy shape not normalised: %+v", j.Input)
			}
			return &JobResponse{Status: "COMPLETED"}, nil
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	body := `{"input": {"image_base64": "Zm9v", "output_format": "jpg"}}`
	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(raw))
	}
}

func TestRunsync_RejectsEmptyInput(t *testing.T) {
	srv := newTestServer(t, &stubProvider{})
	defer srv.Close()

	body := `{"input": {}}`
	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("empty input should be 400, got %d", resp.StatusCode)
	}
}

func TestRunsync_ProviderErrorMapsTo502(t *testing.T) {
	p := &stubProvider{
		runFn: func(JobRequest) (*JobResponse, error) {
			return nil, AsProviderError(errors.New("backend exploded"))
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	body := `{"input": {"images": [{"image_base64": "Zm9v"}]}}`
	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 502 {
		t.Errorf("ProviderError should map to 502, got %d", resp.StatusCode)
	}
}

func TestRunsync_GenericErrorMapsTo500(t *testing.T) {
	p := &stubProvider{
		runFn: func(JobRequest) (*JobResponse, error) {
			return nil, errors.New("client-side decode failure")
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	body := `{"input": {"images": [{"image_base64": "Zm9v"}]}}`
	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Errorf("non-provider error should map to 500, got %d", resp.StatusCode)
	}
}

func TestRunsync_RejectsMethodOther(t *testing.T) {
	srv := newTestServer(t, &stubProvider{})
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/runsync")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("GET /runsync should be 405, got %d", resp.StatusCode)
	}
}

func TestUpscaleAlias(t *testing.T) {
	// `/upscale` is an alias for `/runsync` — same handler. Sanity
	// check that the alias path is registered.
	called := false
	p := &stubProvider{
		runFn: func(JobRequest) (*JobResponse, error) {
			called = true
			return &JobResponse{Status: "COMPLETED"}, nil
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	body := `{"input": {"images": [{"image_base64": "Zm9v"}]}}`
	resp, err := http.Post(srv.URL+"/upscale", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !called {
		t.Errorf("/upscale should reach the same handler as /runsync")
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t, &stubProvider{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/health should be 200, got %d", resp.StatusCode)
	}
}
