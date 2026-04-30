package serve

import (
	"context"
	"errors"
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
	runFn    func([]byte) ([]byte, error)
}

func (s *stubProvider) Start(context.Context) error { return s.startErr }
func (s *stubProvider) Run(_ context.Context, b []byte) ([]byte, error) {
	return s.runFn(b)
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

func TestRunsync_HappyPath_PassesThroughOpaque(t *testing.T) {
	// Whatever the caller posts, the provider sees the SAME bytes.
	// That's the contract — iosuite serve doesn't interpret the
	// inner shape, so different *-serve modules with different input
	// schemas all flow through unchanged.
	caller := []byte(`{"input":{"images":[{"image_base64":"Zm9v"}],"some_future_field":42}}`)
	worker := []byte(`{"status":"COMPLETED","output":{"outputs":[{"image_base64":"YmFy","exec_ms":42}]}}`)

	var saw []byte
	p := &stubProvider{
		runFn: func(b []byte) ([]byte, error) {
			saw = append(saw[:0], b...)
			return worker, nil
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(string(caller)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if string(body) != string(worker) {
		t.Errorf("response body = %q, want %q (opaque pass-through)", body, worker)
	}
	if string(saw) != string(caller) {
		t.Errorf("provider saw = %q, want %q (opaque pass-through)", saw, caller)
	}
}

func TestRunsync_RejectsMissingInput(t *testing.T) {
	p := &stubProvider{runFn: func([]byte) ([]byte, error) {
		t.Fatal("provider should not be called")
		return nil, nil
	}}
	srv := newTestServer(t, p)
	defer srv.Close()

	cases := []string{
		`{}`,                    // no input field
		`{"input": null}`,       // explicit null
		`{"other": {}}`,         // wrong key
	}
	for _, body := range cases {
		resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("body %q: status = %d (want 400), resp = %s", body, resp.StatusCode, respBody)
		}
	}
}

func TestRunsync_RejectsNonJSON(t *testing.T) {
	p := &stubProvider{runFn: func([]byte) ([]byte, error) {
		t.Fatal("provider should not be called")
		return nil, nil
	}}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/runsync", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRunsync_ProviderErrorMapsTo502(t *testing.T) {
	p := &stubProvider{
		runFn: func([]byte) ([]byte, error) {
			return nil, AsProviderError(errors.New("upstream is sad"))
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/runsync", "application/json",
		strings.NewReader(`{"input":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "upstream is sad") {
		t.Errorf("body should contain underlying error: %s", body)
	}
}

func TestRunsync_GenericErrorMapsTo500(t *testing.T) {
	p := &stubProvider{
		runFn: func([]byte) ([]byte, error) {
			return nil, errors.New("internal goof")
		},
	}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/runsync", "application/json",
		strings.NewReader(`{"input":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestRunsync_RejectsMethodOther(t *testing.T) {
	p := &stubProvider{}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/runsync")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestUpscaleAlias(t *testing.T) {
	worker := []byte(`{"status":"COMPLETED","output":{}}`)
	p := &stubProvider{runFn: func([]byte) ([]byte, error) { return worker, nil }}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/upscale", "application/json",
		strings.NewReader(`{"input":{"images":[{"image_base64":"Zm9v"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("/upscale status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestHealth(t *testing.T) {
	p := &stubProvider{}
	srv := newTestServer(t, p)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"ok"`) {
		t.Errorf("body = %s", body)
	}
}
