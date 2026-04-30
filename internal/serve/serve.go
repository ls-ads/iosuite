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
// Wire shape (envelope-only — iosuite is opaque to the inner contents):
//
//	POST /runsync
//	{"input": {<tool-specific fields>}}
//
//	→ {"status": "COMPLETED", "output": {<tool-specific fields>}}
//
// The daemon does NOT interpret `input.*` or `output.*`. Each *-serve
// module owns its own input/output schema; iosuite only insists on
// the `input` envelope key being present (so callers get a clear
// error rather than a confusing pass-through 500 if they post an
// arbitrary blob).
//
// The `/upscale` path is kept as an alias of `/runsync` for callers
// that prefer the more descriptive name.
package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Provider is what /runsync routes to. Implementations own all the
// provider-specific I/O (subprocess management, RunPod HTTP, etc.)
// so the HTTP layer stays trivial — Run sees a raw request body and
// returns a raw response body.
type Provider interface {
	// Start initializes the provider. For LocalProvider this spawns
	// real-esrgan-serve serve and waits for it to be ready. For
	// RunPodProvider this probes the upstream /health.
	Start(ctx context.Context) error

	// Run executes one inference job. Receives the already-validated
	// request body (`{"input": ...}` envelope) and returns the
	// upstream's response body unchanged. Errors that look like
	// upstream failures should be wrapped with AsProviderError so
	// the HTTP layer maps them to 502 instead of 500.
	Run(ctx context.Context, requestBody []byte) ([]byte, error)

	// Close tears the provider down. Must be safe to call multiple
	// times. LocalProvider reaps its subprocess here.
	Close() error
}

// Options is the full configuration surface for the daemon.
type Options struct {
	// Bind address — "127.0.0.1" by default, "0.0.0.0" to expose to
	// the LAN (opt-in only — the dev-friendly default keeps the API
	// off-the-wire).
	Bind string

	// Port for the daemon's HTTP listener. iosuite.io's compose
	// talks to 8312 by convention; LocalProvider uses 8311 for the
	// subprocess so they don't collide.
	Port int

	// Provider is wired by the caller. The caller is NOT responsible
	// for pre-starting it — Run calls Provider.Start so the listener
	// never opens before the backend is ready.
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

// envelopeProbe matches just enough of the request to confirm the
// caller posted the expected `{"input": ...}` envelope. We don't
// decode the inner contents — those are tool-specific and pass
// through to the provider unchanged.
type envelopeProbe struct {
	Input json.RawMessage `json:"input"`
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

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logf("req.read_err id=%s err=%q dur=%s", reqID, err.Error(), time.Since(start))
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}

		// Validate the envelope without interpreting its contents.
		// `{"input": ...}` must be present; what's inside is the
		// tool's contract with its own worker.
		var probe envelopeProbe
		if err := json.Unmarshal(body, &probe); err != nil {
			logf("req.bad_json id=%s err=%q dur=%s", reqID, err.Error(), time.Since(start))
			http.Error(w, fmt.Sprintf("decode JSON envelope: %v", err), http.StatusBadRequest)
			return
		}
		if len(probe.Input) == 0 || bytes.Equal(probe.Input, []byte("null")) {
			logf("req.missing_input id=%s dur=%s", reqID, time.Since(start))
			http.Error(w, `request needs an "input" field at the top level`, http.StatusBadRequest)
			return
		}
		logf("req.dispatch id=%s body_size=%d", reqID, len(body))

		respBody, err := p.Run(r.Context(), body)
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
		logf("req.ok id=%s body_size=%d dur=%s", reqID, len(respBody), time.Since(start))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBody)
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
// errors (bad envelope, oversize input) should be returned as
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
