// Package transform implements `iosuite transform` and the
// mime-aware sugar verbs (`iosuite compress`, `iosuite reframe`,
// etc.) by subprocessing `ffmpeg-serve transform`. Same shape as
// `iosuite upscale` driving `real-esrgan-serve upscale`.
//
// Why subprocess and not HTTP: ffmpeg-serve's `transform` CLI mode
// runs the handler in-process, no daemon spin-up. iosuite drives it
// the same way it drives real-esrgan-serve — fast, no port dance.
// The HTTP daemon stays the right path for hosted iosuite.io use
// where one daemon serves many requests.
package transform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"iosuite.io/internal/runtime"
)

// Options is what the cobra layer fills in. The caller-supplied
// `Params` is whatever the transform's manifest expects; the runner
// JSON-serialises it once for the subprocess.
type Options struct {
	Name   string
	Input  string
	Output string         // empty = ffmpeg-serve auto-derives
	Params map[string]any // transform-specific knobs
	// Aux holds paths to optional secondary input files —
	// subtitle-burn takes an SRT, watermark takes an overlay
	// image, color-lut takes a .cube file. Order is preserved;
	// each entry becomes a `--aux <path>` flag on the subprocess.
	Aux []string

	// Override path to the ffmpeg-serve binary. Empty = use the
	// standard locate flow (PATH / env / sibling-binary).
	RuntimeBin string
}

// Run subprocesses `ffmpeg-serve transform <name>` with the given
// options. Stdout/stderr from the subprocess pass straight through
// so users see the same "✓ <path> (N bytes, M ms)" line ffmpeg-serve
// emits.
func Run(ctx context.Context, opts Options) error {
	if opts.Name == "" {
		return errors.New("transform name is required")
	}
	if opts.Input == "" {
		return errors.New("--input is required")
	}

	bin, err := runtime.LocateFFmpegServe(opts.RuntimeBin)
	if err != nil {
		return err
	}

	args := []string{
		"transform", opts.Name,
		"--input", opts.Input,
	}
	if opts.Output != "" {
		args = append(args, "--output", opts.Output)
	}
	if len(opts.Params) > 0 {
		raw, err := json.Marshal(opts.Params)
		if err != nil {
			return fmt.Errorf("encode params: %w", err)
		}
		args = append(args, "--params", string(raw))
	}
	for _, ap := range opts.Aux {
		args = append(args, "--aux", ap)
	}

	// Trap signals so Ctrl-C kills the child too. Without this the
	// helper subprocess keeps running and ffmpeg can leave a
	// half-written output file that confuses the next invocation.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}
