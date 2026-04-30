// Package upscale implements `iosuite upscale` — the headline command.
//
// All actual GPU work happens in `real-esrgan-serve`; this file just
// resolves flags, derives output paths, and subprocesses that binary
// with the right arguments. Output streaming is pass-through: the
// child's stdout/stderr go straight to ours so progress events,
// errors, and tracebacks all surface in real time.
//
// Round 1 wires only the local provider (subprocess to the binary on
// this host). The `runpod` and `serve` providers slot in at the same
// callsite once they exist; the `--provider` flag is wired now so
// users see the future shape in `--help`.
package upscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"iosuite.io/internal/config"
	"iosuite.io/internal/runtime"
)

// Options holds everything the subcommand needs to run.
type Options struct {
	Input      string
	Output     string // "" → auto-derive
	Model      string
	Provider   string
	GPUID      int
	Tile       bool
	JSONEvents bool

	// `--runtime` override for the real-esrgan-serve binary path.
	// Empty string falls through to the standard lookup.
	RuntimeBin string
}

// Run executes one upscale call. Honours Ctrl-C by forwarding SIGTERM
// to the child subprocess and waiting for it to exit cleanly.
func Run(ctx context.Context, cfg config.Config, opts Options) error {
	if opts.Input == "" {
		return errors.New("--input is required")
	}

	provider := strings.ToLower(opts.Provider)
	if provider == "" {
		provider = cfg.Provider
	}
	switch provider {
	case "local":
		// fall through
	case "runpod", "serve":
		return fmt.Errorf("provider %q is planned but not yet implemented; use --provider local", provider)
	default:
		return fmt.Errorf("unknown provider %q (expected local | runpod | serve)", provider)
	}

	bin, err := runtime.LocateRealEsrganServe(opts.RuntimeBin)
	if err != nil {
		return err
	}

	model := opts.Model
	if model == "" {
		model = cfg.Model
	}

	output := opts.Output
	if output == "" {
		output = derivedOutputPath(opts.Input, cfg.OutputDir)
	}

	args := []string{
		"upscale",
		"--input", opts.Input,
		"--output", output,
		"--model", model,
		"--gpu-id", fmt.Sprintf("%d", opts.GPUID),
	}
	if opts.JSONEvents {
		args = append(args, "--json-events")
	}
	if opts.Tile {
		args = append(args, "--tile")
	}

	// Trap signals so Ctrl-C kills the child too. Without this the
	// helper subprocess keeps running, leaving a half-finished output
	// file that confuses the next invocation.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// Bubble the helper's exit code up to the user's shell
			// so scripts can branch on it. iosuite is documented to
			// use the same exit codes the helper emits.
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}

// derivedOutputPath builds <stem>_4x.<ext> alongside the input, OR in
// the configured OutputDir if set.
func derivedOutputPath(input, outDir string) string {
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".png"
	}
	dir := filepath.Dir(input)
	if outDir != "" {
		dir = outDir
	}
	return filepath.Join(dir, stem+"_4x"+ext)
}

// CopyStreams is exposed for callers that need to wire stdout/stderr
// to non-terminal sinks (e.g., a future `iosuite serve` mode that
// captures helper output to a buffered log). Round 1 doesn't use it
// directly but reserving the helper keeps Run() clean if streaming
// changes later.
func CopyStreams(dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)
	return err
}
