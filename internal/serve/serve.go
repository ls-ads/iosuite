// Package serve implements `iosuite serve` — a long-lived HTTP daemon
// that exposes a stable upscale API regardless of which backend
// actually runs the inference.
//
// The daemon is what iosuite.io's FastAPI backend talks to in Phase 4
// (and what self-hosters point their own apps at). The wire shape
// MIRRORS RunPod's serverless API one-to-one so iosuite.io's existing
// `submit_upscale_batch` works unchanged — the only diff is the URL.
//
//	┌─ iosuite serve (this package) ─┐
//	│   POST /runsync ──────┐        │
//	│   GET  /health        │        │
//	└───────────────────────│────────┘
//	                        ▼
//	         ┌───────────────────────────┐
//	         │ Provider (interface)      │
//	         │   • LocalProvider         │ ← spawns real-esrgan-serve serve
//	         │   • RunPodProvider        │ ← forwards JSON to api.runpod.ai
//	         │   • RemoteProvider (later)│
//	         └───────────────────────────┘
//
// Wire shape (RunPod-compatible):
//
//	POST /runsync
//	{"input": {
//	    "images": [{"image_base64": "..."}, ...],
//	    "output_format": "jpg" | "png",
//	    "tile": true | false
//	}}
//
//	→ {"status": "COMPLETED", "output": {
//	    "outputs": [{"image_base64": "...", "exec_ms": ..., ...}],
//	    "_diagnostics": {...}
//	}}
//
// The `/upscale` path is kept as an alias of `/runsync` for callers
// that prefer the more descriptive name.
package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// JobInput is the inner `input` field of a /runsync request. Mirrors
// the shape real-esrgan-serve's RunPod handler accepts (`BatchPayload`
// in providers/runpod/handler.py). Optional fields default at the
// worker side; we just pass them through.
type JobInput struct {
	Images []ImageInput `json:"images,omitempty"`

	// Legacy single-image fields. Not produced by current iosuite.io
	// callers but accepted so a self-hoster pointing curl at the
	// daemon can use the simpler shape.
	ImageBase64 string `json:"image_base64,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	ImagePath   string `json:"image_path,omitempty"`

	OutputFormat  string `json:"output_format,omitempty"`
	Tile          bool   `json:"tile,omitempty"`
	DiscardOutput bool   `json:"discard_output,omitempty"`
}

// ImageInput is one entry of the `images` array.
type ImageInput struct {
	ImageBase64  string `json:"image_base64,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
	ImagePath    string `json:"image_path,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	OutputPath   string `json:"output_path,omitempty"`
}

// JobRequest is the outer envelope POSTed to /runsync. Mirrors
// RunPod's contract — the `input` field carries the actual job.
type JobRequest struct {
	Input JobInput `json:"input"`
}

// JobOutput is the inner result envelope. RunPod-shaped.
type JobOutput struct {
	Outputs     []ImageOutput   `json:"outputs"`
	Model       string          `json:"model,omitempty"`
	Diagnostics json.RawMessage `json:"_diagnostics,omitempty"`

	// Legacy single-image fields surfaced when the request was a
	// 1-element batch (back-compat with the pre-batched response
	// shape).
	ImageBase64      string `json:"image_base64,omitempty"`
	OutputResolution string `json:"output_resolution,omitempty"`
	InputResolution  string `json:"input_resolution,omitempty"`
}

// ImageOutput is one entry of `outputs[]`.
type ImageOutput struct {
	ImageBase64      string `json:"image_base64,omitempty"`
	OutputPath       string `json:"output_path,omitempty"`
	OutputSizeBytes  int64  `json:"output_size_bytes,omitempty"`
	InputResolution  string `json:"input_resolution,omitempty"`
	OutputResolution string `json:"output_resolution,omitempty"`
	OutputFormat     string `json:"output_format,omitempty"`
	ExecMS           int    `json:"exec_ms,omitempty"`
}

// JobResponse is the full /runsync envelope. `status` mirrors RunPod's
// terminal states; the daemon always returns COMPLETED on success and
// uses HTTP non-2xx for failures so callers can branch on either.
type JobResponse struct {
	Status string    `json:"status"`
	Output JobOutput `json:"output"`
}

// Provider is what /runsync routes to. Implementations own all the
// provider-specific I/O (subprocess management, RunPod HTTP, etc.)
// so the HTTP layer stays trivial.
type Provider interface {
	// Start initializes the provider. For LocalProvider this spawns
	// real-esrgan-serve serve and waits for it to be ready. For
	// RunPodProvider this probes the upstream /health.
	Start(ctx context.Context) error

	// Run executes one inference job. Returns the response envelope.
	// The HTTP layer is responsible only for marshalling +
	// status-code mapping; everything else is the provider's job.
	Run(ctx context.Context, job JobRequest) (*JobResponse, error)

	// Close tears the provider down. Must be safe to call multiple
	// times. LocalProvider reaps its subprocess here.
	Close() error
}

// Options is the full configuration surface for the daemon. Fields
// are filled by the cobra layer from flags + sticky config.
type Options struct {
	// Bind address — "127.0.0.1" by default, "0.0.0.0" to expose to
	// the LAN (opt-in only — the dev-friendly default keeps the API
	// off-the-wire).
	Bind string

	// Port for the daemon's HTTP listener. iosuite.io's compose
	// talks to 8312 by convention; LocalProvider uses 8311 for the
	// subprocess so they don't collide.
	Port int

	// Provider is wired by the caller — Phase 4 ships local + runpod
	// implementations; remote follows. The caller is responsible for
	// passing it in started state? No — Run calls Start so the
	// listener never opens before the backend is ready.
	Provider Provider
}

// Run binds the listener, serves requests, and blocks until SIGINT /
// SIGTERM. Calls Provider.Close() before returning so the subprocess
// (if any) is reaped cleanly.
func Run(ctx context.Context, opts Options) error {
	if opts.Provider == nil {
		return errors.New("serve: no provider configured")
	}
	if opts.Port == 0 {
		opts.Port = 8312
	}
	if opts.Bind == "" {
		opts.Bind = "127.0.0.1"
	}

	if err := opts.Provider.Start(ctx); err != nil {
		return fmt.Errorf("provider start: %w", err)
	}
	defer opts.Provider.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	jobHandler := makeJobHandler(opts.Provider)
	// Both paths point at the same handler — `/runsync` is the
	// RunPod-shaped name (which is what iosuite.io callers use for
	// drop-in compatibility); `/upscale` is the human-friendly
	// alias.
	mux.HandleFunc("/runsync", jobHandler)
	mux.HandleFunc("/upscale", jobHandler)

	addr := fmt.Sprintf("%s:%d", opts.Bind, opts.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	signalCtx, cancel := signal.NotifyContext(ctx,
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-signalCtx.Done()
		fmt.Fprintln(os.Stderr, "iosuite serve: shutting down…")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	fmt.Fprintf(os.Stderr, "iosuite serve listening on http://%s\n", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http: %w", err)
	}
	return nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func makeJobHandler(p Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		// Cap inbound JSON body. 25 MB is plenty for a 4-image batch
		// at ~5 MB raw + base64 overhead — same envelope the
		// real-esrgan-serve worker accepts on the wire.
		const maxBody = 25 * 1024 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)

		// Per-request id + log prefix. With no logging today, a hung
		// upstream is a black box — caller sees a 5xx after minutes,
		// daemon shows nothing. The id lets us correlate with
		// iosuite-api's structured logs when something goes wrong.
		reqID := newReqID()
		start := time.Now()
		path := r.URL.Path
		clen := r.ContentLength
		logf("req.start id=%s path=%s bytes=%d remote=%s", reqID, path, clen, r.RemoteAddr)

		var req JobRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			logf("req.bad_json id=%s err=%q dur=%s", reqID, err.Error(), time.Since(start))
			http.Error(w, fmt.Sprintf("decode JSON: %v", err), http.StatusBadRequest)
			return
		}
		// Normalise legacy single-image input into the array form so
		// providers only deal with one shape downstream.
		if len(req.Input.Images) == 0 {
			if req.Input.ImageBase64 != "" || req.Input.ImageURL != "" || req.Input.ImagePath != "" {
				req.Input.Images = []ImageInput{{
					ImageBase64: req.Input.ImageBase64,
					ImageURL:    req.Input.ImageURL,
					ImagePath:   req.Input.ImagePath,
				}}
				req.Input.ImageBase64 = ""
				req.Input.ImageURL = ""
				req.Input.ImagePath = ""
			}
		}
		if len(req.Input.Images) == 0 {
			logf("req.empty_images id=%s dur=%s", reqID, time.Since(start))
			http.Error(w, `request needs "input.images" (array) or one of input.image_base64/url/path`, http.StatusBadRequest)
			return
		}
		logf("req.dispatch id=%s images=%d tile=%t fmt=%s discard=%t",
			reqID, len(req.Input.Images), req.Input.Tile, req.Input.OutputFormat, req.Input.DiscardOutput)

		resp, err := p.Run(r.Context(), req)
		if err != nil {
			var perr *ProviderError
			if errors.As(err, &perr) {
				logf("req.provider_err id=%s err=%q dur=%s", reqID, perr.Error(), time.Since(start))
				http.Error(w, perr.Error(), http.StatusBadGateway)
				return
			}
			logf("req.internal_err id=%s err=%q dur=%s", reqID, err.Error(), time.Since(start))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logf("req.ok id=%s outputs=%d dur=%s", reqID, len(resp.Output.Outputs), time.Since(start))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// logf — single-line, space-separated key=value to stderr. Loki's
// promtail picks it up directly; humans grep it. Keeping the format
// simple on purpose — JSON would force everything (including the
// quoted err strings) through proper escaping for one consumer that
// could just as easily parse this.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")+" "+format+"\n", args...)
}

// newReqID — 8-byte random id, hex-encoded. Short enough to grep,
// large enough not to collide across an hour of traffic.
func newReqID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never errs on Linux post-init; fall back to a
		// timestamp string so the surface stays valid.
		return fmt.Sprintf("ts%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// ProviderError marks "the backend is unhealthy / failed our request"
// so the HTTP layer can map to 502 instead of 500. Caller-facing
// errors (bad multipart, oversize input) should be returned as
// ordinary errors → 500 → caller can decode and re-classify.
type ProviderError struct {
	Underlying error
}

func (e *ProviderError) Error() string { return "provider: " + e.Underlying.Error() }
func (e *ProviderError) Unwrap() error { return e.Underlying }

// AsProviderError wraps any error with ProviderError so the HTTP
// layer renders it as 502.
func AsProviderError(err error) error {
	if err == nil {
		return nil
	}
	return &ProviderError{Underlying: err}
}
