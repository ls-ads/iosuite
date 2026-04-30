// LocalProvider — `iosuite serve --provider local` backend.
//
// Spawns a long-lived `real-esrgan-serve serve` subprocess on a
// loopback port and forwards JSON requests unchanged. Real-esrgan-serve
// gained a /runsync endpoint in lockstep with this iosuite version so
// local mode and serverless mode share an identical wire shape — the
// LocalProvider is now a trivial HTTP forwarder, no per-tool
// translation logic.
//
// Tile mode: the wrapped subprocess errors on tile=true (tile
// implementation lives in the python RunPod handler, not the Go
// serve binary). Pass-through means the caller gets that error
// surfaced directly.
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
	// Caller (cobra layer) resolves this via internal/runtime.
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
// serve subprocess and forwarding /runsync requests to it.
type LocalProvider struct {
	opts LocalProviderOptions

	cmd        *exec.Cmd
	subURL     string
	httpClient *http.Client

	// exited is closed when cmd.Wait() returns. Lets waitHealthy
	// fail fast on a crashed startup instead of polling for 120 s.
	exited  chan struct{}
	waitErr error

	closeOnce sync.Once
}

// NewLocal returns a configured LocalProvider. Doesn't spawn the
// subprocess yet — call Start() for that.
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
		httpClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Start spawns real-esrgan-serve serve and polls /health until it's
// ready or the context expires. Returns an error if spawn fails or
// if the subprocess never becomes healthy.
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

	healthCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := l.waitHealthy(healthCtx); err != nil {
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

// Run forwards the raw request body to the wrapped subprocess's
// /runsync. The subprocess accepts the same envelope shape iosuite
// serve does (see real-esrgan-serve's internal/server/server.go),
// so no translation is needed.
func (l *LocalProvider) Run(ctx context.Context, requestBody []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.subURL+"/runsync", bytes.NewReader(requestBody))
	if err != nil {
		return nil, AsProviderError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "close") // see iosuite.io frontend proxy.ts

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, AsProviderError(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, AsProviderError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, AsProviderError(fmt.Errorf("subprocess returned %d: %s", resp.StatusCode, string(respBody)))
	}
	return respBody, nil
}

// Close stops the subprocess. Safe to call multiple times.
func (l *LocalProvider) Close() error {
	l.closeOnce.Do(func() {
		if l.cmd == nil || l.cmd.Process == nil {
			return
		}
		select {
		case <-l.exited:
			return
		default:
		}
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
