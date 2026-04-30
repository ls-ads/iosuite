// Package serve implements `iosuite serve` — a long-lived HTTP daemon
// that exposes a stable upscale API regardless of which backend
// actually runs the inference.
//
// The daemon is what iosuite.io's FastAPI backend talks to in Phase 4
// (and what self-hosters point their own apps at). One HTTP shape, one
// auth model, one config — provider routing is internal:
//
//	┌─ iosuite serve (this package) ─┐
//	│   POST /upscale ────────┐      │
//	│   GET  /health          │      │
//	└─────────────────────────│──────┘
//	                          ▼
//	             ┌───────────────────────────┐
//	             │ Provider (interface)      │
//	             │   • LocalProvider         │ ← spawns real-esrgan-serve serve
//	             │   • RunPodProvider (later)│
//	             │   • RemoteProvider (later)│
//	             └───────────────────────────┘
//
// Round 1 of iosuite serve ships LocalProvider only. The interface
// keeps the HTTP layer clean of provider-specific knowledge so adding
// the other two later doesn't churn the request path.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Provider is what HandleUpscale routes to. Implementations own all
// the provider-specific I/O (subprocess management, RunPod HTTP, etc.)
// so the HTTP layer stays trivial.
type Provider interface {
	// Start initializes the provider. For LocalProvider this spawns
	// real-esrgan-serve serve and waits for it to be ready. The
	// returned error is fatal — the daemon won't bind a listener if
	// Start fails (no point answering requests we can't fulfil).
	Start(ctx context.Context) error

	// Upscale runs one inference. Body is the multipart-or-binary
	// upload from the HTTP client; contentType is its content-type
	// header. Returns the upscaled image bytes + the content type to
	// echo back. Implementations are responsible for any per-request
	// concurrency limits — the HTTP layer gates nothing.
	Upscale(ctx context.Context, body io.Reader, contentType string) (output []byte, outputType string, err error)

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

	// Port for the daemon's HTTP listener. iosuite.io's compose talks
	// to 8312 by convention; LocalProvider uses 8311 for the
	// subprocess so they don't collide.
	Port int

	// Provider is wired by the caller — round 1 only ships the local
	// implementation but the interface is in place for runpod / serve
	// later. The caller is responsible for calling Start() before
	// Run() so the HTTP listener never opens before the backend is
	// ready.
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
	mux.HandleFunc("/upscale", makeUpscaleHandler(opts.Provider))

	addr := fmt.Sprintf("%s:%d", opts.Bind, opts.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on signals. Drain in-flight requests; kill
	// hard after 30 s if the worker is wedged so the operator's
	// docker stop never hangs forever.
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

func makeUpscaleHandler(p Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			http.Error(w, "missing Content-Type", http.StatusBadRequest)
			return
		}

		// Cap per-request body size at 25 MB. Multipart overhead +
		// max upload (15 MB) + room for batched submissions; matches
		// what real-esrgan-serve's handler accepts on the request
		// side. r.Body is wrapped with MaxBytesReader so the provider
		// reads from a bounded stream — no DoS via huge upload.
		const maxBody = 25 * 1024 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)

		out, outType, err := p.Upscale(r.Context(), r.Body, ct)
		if err != nil {
			// Distinguish bad requests from upstream failures by the
			// error type. ProviderError is what implementations
			// return when the BACKEND is unhealthy (worth a 502);
			// everything else (decode failure, oversize) is on the
			// caller (502 → 400/422).
			var perr *ProviderError
			if errors.As(err, &perr) {
				http.Error(w, perr.Error(), http.StatusBadGateway)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", outType)
		_, _ = w.Write(out)
	}
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
// layer renders it as 502. Provider implementations call this when
// the backend (subprocess, RunPod, remote) returned a failure.
func AsProviderError(err error) error {
	if err == nil {
		return nil
	}
	return &ProviderError{Underlying: err}
}
