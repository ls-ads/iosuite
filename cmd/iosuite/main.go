// iosuite — the user-facing CLI for the iosuite ecosystem.
//
// Thin Go orchestrator that subprocesses model-specific binaries
// (real-esrgan-serve first, future whisper-serve / sd-serve, etc.).
// CGO-free, cross-compiles cleanly, single binary distribution.
//
// Round 1 ships these subcommands:
//
//	upscale     — run Real-ESRGAN inference (subprocesses real-esrgan-serve)
//	doctor      — diagnose what's installed / missing on this host
//	fetch-model — pull a verified model artefact from GitHub Releases
//	version     — print version + commit
//
// Round 2 adds: serve, benchmark, endpoint deploy/list/destroy.
//
// See ARCHITECTURE.md for the full design.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"iosuite.io/internal/config"
	"iosuite.io/internal/doctor"
	"iosuite.io/internal/runtime"
	"iosuite.io/internal/upscale"
	"iosuite.io/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// usage is the top-level --help. Subcommand-specific help is emitted
// by each subcommand's own flag.FlagSet.
const usage = `iosuite — image / media processing CLI

Usage:
  iosuite <command> [flags]

Commands:
  upscale       Upscale an image or directory (subprocesses real-esrgan-serve)
  doctor        Diagnose this host: PATH, Python, GPU, auth keys
  fetch-model   Download a verified model artefact (forwarded to real-esrgan-serve)
  version       Print version + commit

Run 'iosuite <command> --help' for the full flag surface of any command.
Config: ~/.config/iosuite/config.toml (see ARCHITECTURE.md).`

func run() error {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return nil
	}

	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "-h", "--help", "help":
		fmt.Println(usage)
		return nil

	case "version", "-v", "--version":
		fmt.Printf("iosuite %s (commit %s)\n", version.Version, version.Commit)
		return nil

	case "upscale":
		return cmdUpscale(args)

	case "doctor":
		return cmdDoctor(args)

	case "fetch-model":
		return cmdFetchModel(args)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Println(usage)
		return errors.New("unknown command")
	}
}

func cmdUpscale(args []string) error {
	fs := flag.NewFlagSet("upscale", flag.ExitOnError)
	var (
		input      = fs.String("input", "", "Input image file or directory")
		inputShort = fs.String("i", "", "(alias for --input)")
		output     = fs.String("output", "", "Output path (auto-derived as <stem>_4x.<ext> if omitted)")
		outShort   = fs.String("o", "", "(alias for --output)")
		model      = fs.String("model", "", "Model name (default: realesrgan-x4plus)")
		provider   = fs.String("provider", "", "local | runpod | serve (defaults from config)")
		gpuID      = fs.Int("gpu-id", 0, "GPU device index (-1 = CPU)")
		tile       = fs.Bool("tile", false, "Tile-based inference for inputs >1280²")
		jsonEvents = fs.Bool("json-events", false, "Emit JSON progress events on stdout")
		runtimeBin = fs.String("runtime", "", "Override path to the real-esrgan-serve binary")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite upscale [flags]

Run Real-ESRGAN inference. Subprocesses real-esrgan-serve, which
must be on PATH (or supplied via --runtime).

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	in := first(*input, *inputShort)
	out := first(*output, *outShort)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	return upscale.Run(context.Background(), cfg, upscale.Options{
		Input:      in,
		Output:     out,
		Model:      *model,
		Provider:   *provider,
		GPUID:      *gpuID,
		Tile:       *tile,
		JSONEvents: *jsonEvents,
		RuntimeBin: *runtimeBin,
	})
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite doctor

Probe this host for everything iosuite needs. Exits non-zero when a
required check fails; optional checks (e.g. RunPod credentials) emit
warnings but don't affect the exit code.`)
	}
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !doctor.Run(context.Background(), os.Stdout, cfg) {
		// Use exit code 3 (environment error) per the documented
		// iosuite exit-code contract — same as real-esrgan-serve.
		os.Exit(3)
	}
	return nil
}

// cmdFetchModel forwards every flag to `real-esrgan-serve fetch-model`
// unmodified. Single source of truth for fetch + verify lives in
// real-esrgan-serve; iosuite shouldn't reimplement it.
func cmdFetchModel(args []string) error {
	bin, err := runtime.LocateRealEsrganServe("")
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, append([]string{"fetch-model"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}

// first returns the leftmost non-empty string. Used to combine long
// + short flag aliases (`--input` / `-i`) without making the user
// remember which form they passed.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
