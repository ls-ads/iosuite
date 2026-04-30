// LocalProvider — `iosuite serve --provider local` backend.
//
// Manages a long-lived `real-esrgan-serve serve` subprocess on a
// loopback port and proxies HTTP /upscale requests to it. The wrapped
// daemon does the actual GPU work (warm ORT/TRT session, the same
// thing iosuite.io's backend uses today via RunPod). iosuite serve
// just re-exposes that on a stable, authentication-friendly endpoint.
//
// Why wrap it instead of forwarding directly:
//   - Stable HTTP shape regardless of backend (round 2 adds runpod /
//     remote providers that look identical from the client's side).
//   - Provider abstraction means iosuite.io can deploy a single
//     `iosuite serve` and decide local-vs-cloud via flag, not by
//     swapping endpoints in the FastAPI config.
//   - Adding cross-cutting concerns (rate-limit, auth, telemetry) goes
//     here once, not per-provider.
package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// LocalProviderOptions configures the wrapped subprocess.
type LocalProviderOptions struct {
	// Bin is the absolute path to the real-esrgan-serve binary.
	// Caller (cobra layer) resolves this via internal/runtime so the
	// provider doesn't need to know about PATH search.
	Bin string

	// SubprocessPort is the loopback port real-esrgan-serve serve
	// binds to. Defaults to 8311 — different from the daemon's own
	// port (default 8312) so they don't collide on a single host.
	SubprocessPort int

	// Model is the model name to keep warm in the subprocess. Empty
	// falls back to "realesrgan-x4plus".
	Model string

	// GPUID — passed through to real-esrgan-serve serve.
	GPUID int
}

// LocalProvider implements Provider by spawning a real-esrgan-serve
// serve subprocess and reverse-proxying HTTP traffic to it.
type LocalProvider struct {
	opts LocalProviderOptions

	cmd        *exec.Cmd
	subURL     string
	httpClient *http.Client

	// exited is closed when cmd.Wait() returns. waitHealthy selects
	// on it so a subprocess that dies during startup (e.g. missing
	// upscaler.py, missing CUDA libs) surfaces as a fast error
	// instead of "did not return 200 within 120 s".
	exited chan struct{}
	waitErr error

	closeOnce sync.Once
}

// NewLocal returns a configured LocalProvider. Doesn't spawn the
// subprocess yet — call Start() for that. Splitting init from start
// makes wiring easier in main() (build the provider, then pass to
// serve.Run which calls Start).
func NewLocal(opts LocalProviderOptions) *LocalProvider {
	if opts.SubprocessPort == 0 {
		opts.SubprocessPort = 8311
	}
	if opts.Model == "" {
		opts.Model = "realesrgan-x4plus"
	}
	return &LocalProvider{
		opts:       opts,
		subURL:     fmt.Sprintf("http://127.0.0.1:%d", opts.SubprocessPort),
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Start spawns real-esrgan-serve serve and polls /health until it's
// ready or the context expires. Returns an error if spawn fails or
// if the subprocess never becomes healthy — Run treats this as fatal
// and won't open the public listener.
func (l *LocalProvider) Start(ctx context.Context) error {
	if l.opts.Bin == "" {
		return errors.New("LocalProvider: Bin must be set (resolve via internal/runtime first)")
	}

	args := []string{
		"serve",
		"--bind", "127.0.0.1",
		"--port", strconv.Itoa(l.opts.SubprocessPort),
		"--model", l.opts.Model,
		"--gpu-id", strconv.Itoa(l.opts.GPUID),
	}
	cmd := exec.Command(l.opts.Bin, args...)
	// Stream the helper's stderr through ours so engine warm-up
	// progress and errors show up in the daemon's logs immediately.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s: %w", l.opts.Bin, err)
	}
	l.cmd = cmd
	l.exited = make(chan struct{})
	go func() {
		l.waitErr = cmd.Wait()
		close(l.exited)
	}()

	// Wait for /health. Cold engine load can take 5–60 s on TRT;
	// 120 s ceiling matches the cold-start budget the iosuite.io
	// backend already plans around. waitHealthy also detects
	// subprocess exit (closes l.exited) so a crashed startup
	// surfaces in seconds, not 120 s.
	healthCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := l.waitHealthy(healthCtx); err != nil {
		// Health never came up. If the subprocess hasn't exited yet
		// (still alive but not responding), kill it so we don't
		// leave an orphan when Run errors.
		select {
		case <-l.exited:
			// already gone
		default:
			_ = cmd.Process.Kill()
			<-l.exited
		}
		return err
	}
	return nil
}

func (l *LocalProvider) waitHealthy(ctx context.Context) error {
	url := l.subURL + "/health"
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		// One attempt before the first tick so a fast subprocess
		// start (warm cache) doesn't pay an unnecessary 500 ms wait.
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := l.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-l.exited:
			// Subprocess died before health came up — surface the
			// wait() error (typically the exit code + a short
			// reason). Saves the operator 120 s of polling on a
			// configuration error like missing upscaler.py.
			if l.waitErr != nil {
				return fmt.Errorf("subprocess exited during startup: %w", l.waitErr)
			}
			return fmt.Errorf("subprocess exited during startup")
		case <-ctx.Done():
			return fmt.Errorf("subprocess /health did not return 200 within timeout: last err %v", err)
		case <-tick.C:
		}
	}
}

// Upscale forwards the request to the wrapped subprocess. The body
// is forwarded as-is (multipart with `image` field, matching
// real-esrgan-serve's contract). Returns the raw output bytes +
// content-type header from the subprocess response.
func (l *LocalProvider) Upscale(ctx context.Context, body io.Reader, contentType string) ([]byte, string, error) {
	// Buffer the request body. The subprocess expects
	// `multipart/form-data` with an `image` field; the caller
	// (iosuite serve's HTTP handler) hands us the raw inbound stream
	// which is the same shape real-esrgan-serve already accepts —
	// so we forward unchanged. Buffering means we can retry once on
	// transient subprocess errors (round 2 enhancement); for now it
	// just simplifies the http.Request construction.
	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, "", fmt.Errorf("read inbound body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.subURL+"/upscale", bytes.NewReader(buf))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Connection", "close") // see proxy.ts in iosuite.io frontend for the same pattern

	resp, err := l.httpClient.Do(req)
	if err != nil {
		// Network-level failure to the subprocess → backend unhealthy.
		// Wrap as ProviderError so the HTTP layer maps to 502.
		return nil, "", AsProviderError(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", AsProviderError(fmt.Errorf("read subprocess response: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		// Subprocess returned 4xx/5xx — surface its body so the
		// caller sees the actual diagnostic. Wrap as ProviderError
		// so 502 propagates upstream.
		return nil, "", AsProviderError(fmt.Errorf(
			"subprocess returned %d: %s", resp.StatusCode, string(respBody)))
	}
	outType := resp.Header.Get("Content-Type")
	if outType == "" {
		outType = "application/octet-stream"
	}
	return respBody, outType, nil
}

// Close stops the subprocess. Safe to call multiple times. Idempotent
// against a subprocess that already exited on its own (e.g. crashed
// during Start) — the exited channel disambiguates.
func (l *LocalProvider) Close() error {
	l.closeOnce.Do(func() {
		if l.cmd == nil || l.cmd.Process == nil {
			return
		}
		// Already exited (crash during startup, or operator killed
		// it externally) — nothing to do, the wait goroutine has
		// already collected the status into waitErr.
		select {
		case <-l.exited:
			return
		default:
		}
		// Try a graceful shutdown first; SIGKILL after 5 s if it's
		// still alive. Same pattern real-esrgan-serve's helperProc
		// uses internally.
		_ = l.cmd.Process.Signal(os.Interrupt)
		select {
		case <-l.exited:
		case <-time.After(5 * time.Second):
			_ = l.cmd.Process.Kill()
			<-l.exited
		}
	})
	return l.waitErr
}
