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
	"time"

	"encoding/base64"
	"strings"

	"iosuite.io/internal/benchmark"
	"iosuite.io/internal/config"
	"iosuite.io/internal/doctor"
	"iosuite.io/internal/endpoint"
	"iosuite.io/internal/manifest"
	"iosuite.io/internal/registry"
	"iosuite.io/internal/runtime"
	"iosuite.io/internal/serve"
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
  serve         Long-lived HTTP daemon (warm engine; what iosuite.io talks to)
  endpoint      Manage remote provider endpoints (deploy / list / destroy)
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

	case "serve":
		return cmdServe(args)

	case "endpoint":
		return cmdEndpoint(args)

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

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var (
		bind       = fs.String("bind", "127.0.0.1", "Bind address (use 0.0.0.0 to expose on LAN)")
		port       = fs.Int("port", 8312, "TCP port to bind")
		provider   = fs.String("provider", "", "local | runpod | serve (defaults from config)")
		model      = fs.String("model", "", "Model to keep warm (default: realesrgan-x4plus)")
		gpuID      = fs.Int("gpu-id", 0, "GPU device index (-1 = CPU)")
		runtimeBin = fs.String("runtime", "", "Override path to the real-esrgan-serve binary (local provider only)")
		subPort    = fs.Int("subprocess-port", 8311, "Loopback port for the wrapped real-esrgan-serve serve (local provider only)")
		// RunPod provider flags
		endpointID   = fs.String("endpoint-id", "", "RunPod endpoint id (runpod provider only)")
		runpodAPIKey = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
		tile         = fs.Bool("tile", true, "Enable worker-side tiling for inputs >1280² (runpod provider only)")
		// PollMax — how long the daemon waits on a single RunPod job
		// before giving up. 10m default; bump for slow / cold-prone
		// endpoints. Falls back to IOSUITE_POLL_MAX env when the flag
		// isn't passed (matches RUNPOD_API_KEY's resolution pattern).
		pollMax = fs.Duration("poll-max", 0, "Max wait per upstream job, e.g. 10m (default 10m, env IOSUITE_POLL_MAX)")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite serve [flags]

Run iosuite as a long-lived HTTP daemon. Holds a warm inference
session so consumers (iosuite.io's backend, your own apps) avoid the
cold-start cost on every request.

Endpoints:
  POST /upscale      multipart/form-data with 'image' field, returns image bytes
  GET  /health       {"status":"ok"} when the backend is reachable

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	prov := *provider
	if prov == "" {
		prov = cfg.Provider
	}
	switch prov {
	case "local":
		bin, err := runtime.LocateRealEsrganServe(*runtimeBin)
		if err != nil {
			return err
		}
		modelName := *model
		if modelName == "" {
			modelName = cfg.Model
		}
		local := serve.NewLocal(serve.LocalProviderOptions{
			Bin:            bin,
			SubprocessPort: *subPort,
			Model:          modelName,
			GPUID:          *gpuID,
		})
		return serve.Run(context.Background(), serve.Options{
			Bind:     *bind,
			Port:     *port,
			Provider: local,
		})
	case "runpod":
		eid := *endpointID
		if eid == "" {
			eid = cfg.RunpodEndpointID
		}
		if eid == "" {
			eid = os.Getenv("RUNPOD_ENDPOINT_ID")
		}
		if eid == "" {
			return fmt.Errorf("runpod provider requires --endpoint-id (or RUNPOD_ENDPOINT_ID env, or [runpod] endpoint_id in config)")
		}
		key := resolveRunpodAPIKey(*runpodAPIKey, cfg)
		if key == "" {
			return fmt.Errorf("runpod provider requires API key (--runpod-api-key, RUNPOD_API_KEY env, or [runpod] api_key in config)")
		}
		// Tile is now per-request — caller sets it in the JobRequest
		// payload so a single daemon can serve mixed traffic. The
		// --tile flag is kept for compat but no longer applies at
		// daemon level; document accordingly.
		_ = *tile
		pm := *pollMax
		if pm == 0 {
			if env := os.Getenv("IOSUITE_POLL_MAX"); env != "" {
				parsed, err := time.ParseDuration(env)
				if err != nil {
					return fmt.Errorf("invalid IOSUITE_POLL_MAX %q: %w", env, err)
				}
				pm = parsed
			}
		}
		rp := serve.NewRunPod(serve.RunPodProviderOptions{
			EndpointID: eid,
			APIKey:     key,
			PollMax:    pm,
		})
		return serve.Run(context.Background(), serve.Options{
			Bind:     *bind,
			Port:     *port,
			Provider: rp,
		})
	case "serve":
		return fmt.Errorf("provider %q is planned but not yet implemented; use --provider local or --provider runpod", prov)
	default:
		return fmt.Errorf("unknown provider %q (expected local | runpod | serve)", prov)
	}
}

// cmdEndpoint dispatches `iosuite endpoint <subcommand>`. Sub-subs
// (deploy / list / destroy / benchmark) parse their own flag sets.
func cmdEndpoint(args []string) error {
	if len(args) == 0 {
		fmt.Println(`Usage: iosuite endpoint <subcommand> [flags]

Subcommands:
  deploy     Create or update a serverless endpoint on a provider
  list       List existing endpoints
  destroy    Delete an endpoint
  benchmark  Run the tool's published benchmark suite against an endpoint

Each subcommand accepts --provider runpod (the only supported provider
in this round). RunPod credentials come from --runpod-api-key flag,
$RUNPOD_API_KEY env, or [runpod] api_key in ~/.config/iosuite/config.toml.`)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "deploy":
		return cmdEndpointDeploy(rest)
	case "list":
		return cmdEndpointList(rest)
	case "destroy":
		return cmdEndpointDestroy(rest)
	case "benchmark":
		return cmdEndpointBenchmark(rest)
	case "-h", "--help", "help":
		return cmdEndpoint(nil)
	default:
		return fmt.Errorf("unknown endpoint subcommand: %s", sub)
	}
}

func cmdEndpointDeploy(args []string) error {
	fs := flag.NewFlagSet("endpoint deploy", flag.ExitOnError)
	var (
		provider    = fs.String("provider", "runpod", "Provider (runpod is the only supported value today)")
		tool        = fs.String("tool", "real-esrgan", "Tool to deploy (real-esrgan)")
		gpuClass    = fs.String("gpu-class", "rtx-4090", "GPU class — rtx-4090, rtx-3090, l40s, etc.")
		name        = fs.String("name", "", "Endpoint name (default: <tool>-<gpu-class>)")
		apiKey      = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
		workersMax  = fs.Int("workers-max", 0, "Max concurrent workers (0 = tool default)")
		idleTimeout = fs.Int("idle-timeout", 0, "Worker idle timeout in seconds (0 = tool default)")
		// Tri-state flag: --flashboot, --no-flashboot, or unset (use
		// tool default). Go's flag package only does true booleans;
		// we model the unset case by walking fs.Visit() after Parse.
		flashboot      = fs.Bool("flashboot", true, "Enable RunPod FlashBoot (snapshot resume) for faster cold starts")
		minCudaVersion = fs.String("min-cuda", "", "Pin workers to RunPod hosts with NVIDIA driver supporting this CUDA version or newer (e.g. 12.8). Empty = use the manifest's value")
		// Manifest resolution. --version selects the git tag of the
		// *-serve repo whose deploy/runpod.json iosuite reads. Empty
		// = the registry's StableVersion. --manifest overrides the
		// fetch entirely with a local file (dev workflow before
		// pushing the manifest to the serve repo).
		manifestVersion = fs.String("version", "", "Git tag of the *-serve repo to read the manifest from (default: registry's stable version)")
		manifestPath    = fs.String("manifest", "", "Read deploy manifest from a local file instead of fetching by tool+version (dev override)")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite endpoint deploy [flags]

Create (or update) a serverless endpoint on a provider. Idempotent:
re-running with the same name updates the template + endpoint
in-place.

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	key := resolveRunpodAPIKey(*apiKey, cfg)

	// Resolve the deploy manifest before we touch RunPod. Local file
	// wins (dev override); otherwise fetch from the *-serve repo at
	// the requested git tag.
	ctx := context.Background()
	var (
		man          *manifest.Manifest
		manifestSrc  string
	)
	if *manifestPath != "" {
		man, err = manifest.LoadFile(*manifestPath)
		if err != nil {
			return err
		}
		manifestSrc = *manifestPath
	} else {
		url, err := registry.ManifestURL(*tool, *manifestVersion)
		if err != nil {
			return err
		}
		man, err = manifest.Fetch(ctx, url)
		if err != nil {
			return err
		}
		manifestSrc = url
	}

	// Distinguish "user passed --flashboot=…" from "user didn't pass
	// it"; the endpoint package needs the latter to fall back to the
	// manifest default. flag.Visit walks only flags that were set.
	var flashbootSet bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "flashboot" {
			flashbootSet = true
		}
	})
	in := endpoint.DeployInput{
		Provider:       *provider,
		Tool:           *tool,
		GPUClass:       *gpuClass,
		Name:           *name,
		APIKey:         key,
		Manifest:       man,
		WorkersMax:     *workersMax,
		IdleTimeoutS:   *idleTimeout,
		MinCudaVersion: *minCudaVersion,
		UserAgent:      fmt.Sprintf("iosuite/%s", version.Version),
	}
	if flashbootSet {
		in.Flashboot = flashboot
	}
	res, err := endpoint.Deploy(ctx, in)
	if err != nil {
		return err
	}
	res.ManifestSource = manifestSrc
	endpoint.PrintDeploy(os.Stdout, res)
	return nil
}

func cmdEndpointList(args []string) error {
	fs := flag.NewFlagSet("endpoint list", flag.ExitOnError)
	var (
		provider = fs.String("provider", "runpod", "Provider")
		apiKey   = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	endpoints, err := endpoint.List(context.Background(), *provider,
		resolveRunpodAPIKey(*apiKey, cfg),
		fmt.Sprintf("iosuite/%s", version.Version))
	if err != nil {
		return err
	}
	if len(endpoints) == 0 {
		fmt.Println("(no endpoints on this account)")
		return nil
	}
	for _, e := range endpoints {
		fmt.Printf("  %s  %s  template=%s\n", e.ID, e.Name, e.TemplateID)
	}
	return nil
}

func cmdEndpointDestroy(args []string) error {
	fs := flag.NewFlagSet("endpoint destroy", flag.ExitOnError)
	var (
		provider = fs.String("provider", "runpod", "Provider")
		name     = fs.String("name", "", "Endpoint name (alternative to passing the id positionally)")
		apiKey   = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := ""
	if fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	deleted, err := endpoint.Destroy(context.Background(), *provider,
		resolveRunpodAPIKey(*apiKey, cfg),
		fmt.Sprintf("iosuite/%s", version.Version),
		id, *name)
	if err != nil {
		return err
	}
	fmt.Printf("deleted endpoint: %s\n", deleted)
	return nil
}

// cmdEndpointBenchmark drives the workload declared in a *-serve
// module's deploy/benchmark.json against a deployed endpoint.
// iosuite owns the wire (POST loop, timing, percentile math); the
// serve module owns what to send and what to measure.
func cmdEndpointBenchmark(args []string) error {
	fs := flag.NewFlagSet("endpoint benchmark", flag.ExitOnError)
	var (
		provider          = fs.String("provider", "runpod", "Provider (runpod is the only supported value today)")
		tool              = fs.String("tool", "real-esrgan", "Tool whose benchmark manifest to run")
		endpointID        = fs.String("endpoint-id", "", "RunPod endpoint id to benchmark (required)")
		apiKey            = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
		manifestVersion   = fs.String("version", "", "Git tag of the *-serve repo to read the benchmark manifest from (default: registry's stable version)")
		benchmarkPath     = fs.String("benchmark-manifest", "", "Read benchmark manifest from a local file instead of fetching by tool+version")
		inputResourcePath = fs.String("input-resource", "", "Read the benchmark input from a local file instead of fetching from the *-serve repo (paired with --benchmark-manifest for offline dev)")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite endpoint benchmark [flags]

Run a tool's published benchmark workload against a deployed
endpoint. The serve module owns the workload (input image, request
shape, metrics); iosuite owns the wire.

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *provider != "runpod" {
		return fmt.Errorf("provider %q is not supported (only 'runpod' is implemented)", *provider)
	}
	if *endpointID == "" {
		return fmt.Errorf("--endpoint-id is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	key := resolveRunpodAPIKey(*apiKey, cfg)
	if key == "" {
		return fmt.Errorf("RunPod API key required (--runpod-api-key, RUNPOD_API_KEY, or [runpod] api_key in config)")
	}

	ctx := context.Background()

	// Resolve the benchmark manifest. Local file wins (dev override);
	// otherwise fetch from the *-serve repo at the requested tag.
	var (
		bench       *manifest.BenchmarkManifest
		bSource     string
		baseURL     string
		fetchedFile bool
	)
	if *benchmarkPath != "" {
		bench, err = manifest.LoadBenchmarkFile(*benchmarkPath)
		if err != nil {
			return err
		}
		bSource = *benchmarkPath
		// For input-resource resolution we use a file:// scheme so
		// FetchInputResource reads from disk relative to the manifest.
		// Caller may pass --input-resource explicitly to skip that.
		baseURL = "file://" + *benchmarkPath
	} else {
		url, err := registry.BenchmarkURL(*tool, *manifestVersion)
		if err != nil {
			return err
		}
		bench, err = manifest.FetchBenchmark(ctx, url)
		if err != nil {
			return err
		}
		bSource = url
		baseURL = url
		fetchedFile = true
	}
	_ = fetchedFile

	// Fetch the input image. --input-resource takes precedence; else
	// resolve relative to the manifest's source.
	var inputBytes []byte
	if *inputResourcePath != "" {
		inputBytes, err = os.ReadFile(*inputResourcePath)
		if err != nil {
			return fmt.Errorf("read --input-resource: %w", err)
		}
	} else {
		inputBytes, err = manifest.FetchInputResource(ctx, baseURL, bench.InputResource)
		if err != nil {
			return err
		}
	}

	// inputBytes can be either raw image bytes OR an already-base64
	// string (real-esrgan-serve ships its bench input pre-encoded).
	// Detect the b64 case so we don't double-encode in buildRequestBody.
	if isLikelyBase64(inputBytes) {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(inputBytes)))
		if err == nil {
			inputBytes = decoded
		}
	}

	fmt.Printf("benchmark: tool=%s endpoint=%s warmup=%d measure=%d\n",
		bench.Tool, *endpointID, bench.Warmup, bench.Measure)
	fmt.Printf("manifest:  %s\n", bSource)
	fmt.Println()

	results, err := benchmark.Run(ctx, *endpointID, key, bench, inputBytes)
	if err != nil {
		return err
	}
	fmt.Print(benchmark.FormatResults(results))
	return nil
}

// isLikelyBase64 returns true if the input looks like base64-encoded
// text (single line, only base64 alphabet chars). Real-esrgan-serve's
// deploy/bench/*.b64 files ship in this form; a future tool might
// ship raw bytes instead — both should work without operator
// intervention.
func isLikelyBase64(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if s == "" || len(s) < 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r' {
			continue
		}
		return false
	}
	return true
}

// resolveRunpodAPIKey applies the precedence: flag > env > config.
// Empty return is a soft signal — let the underlying call surface
// the actionable error (which mentions all three sources).
func resolveRunpodAPIKey(flagVal string, cfg config.Config) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv("RUNPOD_API_KEY"); env != "" {
		return env
	}
	return cfg.RunpodAPIKey
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
