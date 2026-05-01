// Package upscale implements `iosuite super-resolution` — the AI
// super-resolution command (legacy package name kept to avoid churn;
// the CLI verb is `super-resolution`).
//
// All actual GPU work happens in `real-esrgan-serve`; this file just
// resolves flags, derives output paths, and subprocesses that binary
// with the right arguments. Output streaming is pass-through: the
// child's stdout/stderr go straight to ours so progress events,
// errors, and tracebacks all surface in real time.
//
// Local-only today. Sending an upscale to a remote endpoint goes
// through `iosuite serve --provider runpod` instead.
package upscale

import (
	"context"
	"errors"
	"fmt"
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
	if provider != "local" {
		return fmt.Errorf("`iosuite super-resolution` is local-only (got --provider %q). For remote inference, run `iosuite serve --provider runpod` and POST to the daemon.", provider)
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
		"super-resolution",
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

