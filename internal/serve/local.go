// LocalProvider — `iosuite serve --provider local` backend.
//
// Manages a long-lived `real-esrgan-serve serve` subprocess on a
// loopback port and translates the daemon's JSON JobRequest into the
// multipart calls real-esrgan-serve serve expects (one HTTP call per
// image in the batch). The wrapped daemon does the actual GPU work
// (warm ORT/TRT session, the same thing iosuite.io's backend uses
// today via RunPod). iosuite serve just re-exposes that on a stable,
// authentication-friendly endpoint.
//
// Single-image-per-call, no tile mode: real-esrgan-serve serve's HTTP
// surface is single-shot multipart with no tile flag. Tile-mode
// uploads (>1280²) need to go through `--provider runpod`, where the
// worker handler accepts the batched JSON shape natively. Documented
// in iosuite's ARCHITECTURE.md.
package serve

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
// serve subprocess and translating Run() requests into one or more
// HTTP calls against it.
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
		httpClient: &http.Client{Timeout: 5 * time.Minute},
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

// Run translates the JobRequest into N single-image HTTP calls
// against the wrapped subprocess and aggregates the results into a
// JobResponse. real-esrgan-serve serve's HTTP API is single-shot per
// request (one multipart upload, one image back), so a multi-image
// JobRequest fans out into N calls.
//
// LocalProvider does NOT honor tile=true — real-esrgan-serve serve's
// HTTP path doesn't accept it. Callers that need tiling should use
// `iosuite serve --provider runpod` instead.
func (l *LocalProvider) Run(ctx context.Context, job JobRequest) (*JobResponse, error) {
	if len(job.Input.Images) == 0 {
		return nil, errors.New("Run: no images in request")
	}
	outputs := make([]ImageOutput, 0, len(job.Input.Images))
	for i, img := range job.Input.Images {
		raw, err := decodeImageInput(img)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i, err)
		}
		t0 := time.Now()
		outBytes, err := l.upscaleOne(ctx, raw)
		execMS := int(time.Since(t0).Milliseconds())
		if err != nil {
			return nil, AsProviderError(fmt.Errorf("upscale image %d: %w", i, err))
		}
		outputs = append(outputs, ImageOutput{
			ImageBase64:  base64.StdEncoding.EncodeToString(outBytes),
			OutputFormat: defaultStr(img.OutputFormat, defaultStr(job.Input.OutputFormat, "jpg")),
			ExecMS:       execMS,
		})
	}
	return &JobResponse{
		Status: "COMPLETED",
		Output: JobOutput{Outputs: outputs},
	}, nil
}

// upscaleOne does ONE multipart POST to the wrapped subprocess.
// Returns the raw image bytes from the response body.
func (l *LocalProvider) upscaleOne(ctx context.Context, imgBytes []byte) ([]byte, error) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("image", "input.bin")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(imgBytes); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.subURL+"/upscale", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Connection", "close") // see iosuite.io frontend proxy.ts

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subprocess returned %d: %s", resp.StatusCode, string(respBody))
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

// decodeImageInput pulls the image bytes out of an ImageInput. Only
// image_base64 is supported in this round; image_url and image_path
// would require fetching them, which is the runpod handler's job
// (upstream services like requests.get + workspace mount). Local
// callers can base64-encode upfront.
func decodeImageInput(img ImageInput) ([]byte, error) {
	if img.ImageBase64 != "" {
		// Strip a `data:image/png;base64,` prefix if present.
		s := img.ImageBase64
		if idx := indexByte(s, ','); idx >= 0 && hasPrefix(s, "data:") {
			s = s[idx+1:]
		}
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("decode image_base64: %w", err)
		}
		return raw, nil
	}
	if img.ImageURL != "" || img.ImagePath != "" {
		return nil, errors.New("image_url and image_path are only supported with --provider runpod (the worker handler fetches them); use image_base64 with --provider local")
	}
	return nil, errors.New("image input is empty")
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
