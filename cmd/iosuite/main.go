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
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"iosuite.io/internal/benchmark"
	"iosuite.io/internal/config"
	"iosuite.io/internal/doctor"
	"iosuite.io/internal/endpoint"
	"iosuite.io/internal/manifest"
	"iosuite.io/internal/registry"
	"iosuite.io/internal/runtime"
	"iosuite.io/internal/serve"
	"iosuite.io/internal/transform"
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

Media transforms (mime-aware; same verb works on image / video / audio):
  upscale       Upscale (real-esrgan AI by default; --method= for lanczos/bicubic/...)
  compress      Shrink to a size target (Discord/WhatsApp/X presets) or quality target
  convert       Change format (jpg ↔ webp ↔ avif ↔ ..., mp4 ↔ webm ↔ mov, mp3 ↔ flac ...)
  reframe       Change aspect ratio (16:9 ↔ 9:16 ↔ 1:1 ...) with blur-pad / letterbox / crop
  normalize     EBU R128 audio loudness normalization
  transform     Generic dispatch for any registered transform (escape hatch)

Infrastructure:
  serve         Long-lived HTTP daemon (warm engine; what iosuite.io talks to)
  endpoint      Manage remote provider endpoints (deploy / list / destroy / benchmark)
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

	case "compress":
		return cmdSugar("compress", args)
	case "convert":
		return cmdSugar("convert", args)
	case "reframe":
		return cmdSugar("reframe", args)
	case "normalize":
		return cmdSugar("normalize", args)
	case "transform":
		return cmdTransform(args)

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
		output     = fs.String("output", "", "Output path (auto-derived if omitted)")
		outShort   = fs.String("o", "", "(alias for --output)")
		model      = fs.String("model", "", "Model name (default: realesrgan-x4plus); only applies to --method=real-esrgan")
		provider   = fs.String("provider", "", "local | runpod (defaults from config); only applies to --method=real-esrgan")
		gpuID      = fs.Int("gpu-id", 0, "GPU device index (-1 = CPU); only applies to --method=real-esrgan")
		tile       = fs.Bool("tile", false, "Tile-based inference for inputs >1280²; only applies to --method=real-esrgan")
		jsonEvents = fs.Bool("json-events", false, "Emit JSON progress events on stdout; only applies to --method=real-esrgan")
		method     = fs.String("method", "real-esrgan", "real-esrgan | lanczos | bicubic | bilinear | neighbor")
		scale      = fs.Float64("scale", 0, "Upscale factor (default 4 for AI, 4 for resampling). Resampling only.")
		runtimeBin = fs.String("runtime", "", "Override path to the *-serve binary (real-esrgan-serve OR ffmpeg-serve depending on --method)")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: iosuite upscale [flags] [path]

Upscale an image. Two engines:

  --method=real-esrgan  (default)  AI super-resolution (TensorRT-
                                   accelerated). 4× only. Subprocesses
                                   real-esrgan-serve.
  --method=lanczos|bicubic|bilinear|neighbor
                                   Classical resampling. CPU-only,
                                   fast, deterministic. Free tier on
                                   iosuite.io. Subprocesses
                                   ffmpeg-serve. neighbor is for
                                   pixel-art.

Flags:`)
		fs.PrintDefaults()
	}
	pos := parseInterspersed(fs, args)

	// Positional <path> is the same as --input. Sugar for the common case.
	in := first(*input, *inputShort)
	if in == "" {
		in = pos
	}
	out := first(*output, *outShort)

	switch *method {
	case "", "real-esrgan":
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
	case "lanczos", "bicubic", "bilinear", "neighbor":
		s := *scale
		if s == 0 {
			s = 4
		}
		params := map[string]any{
			"method": *method,
			"scale":  s,
		}
		return transform.Run(context.Background(), transform.Options{
			Name:       "upscale",
			Input:      in,
			Output:     out,
			Params:     params,
			RuntimeBin: *runtimeBin,
		})
	default:
		return fmt.Errorf("unknown --method %q (expected real-esrgan | lanczos | bicubic | bilinear | neighbor)", *method)
	}
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
	if !doctor.Run(os.Stdout, cfg) {
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
		provider   = fs.String("provider", "", "local | runpod (defaults from config)")
		model      = fs.String("model", "", "Model to keep warm (default: realesrgan-x4plus)")
		gpuID      = fs.Int("gpu-id", 0, "GPU device index (-1 = CPU)")
		runtimeBin = fs.String("runtime", "", "Override path to the real-esrgan-serve binary (local provider only)")
		subPort    = fs.Int("subprocess-port", 8311, "Loopback port for the wrapped real-esrgan-serve serve (local provider only)")
		// RunPod provider flags
		endpointID   = fs.String("endpoint-id", "", "RunPod endpoint id (runpod provider only)")
		runpodAPIKey = fs.String("runpod-api-key", "", "RunPod API key (overrides env + config)")
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
  POST /runsync     application/json envelope ({"input": ...}); /upscale alias
  GET  /health      {"status":"ok"} when the backend is reachable

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
	default:
		return fmt.Errorf("unknown provider %q (expected local | runpod)", prov)
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
		bench   *manifest.BenchmarkManifest
		bSource string
		baseURL string
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
	}

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

// cmdTransform runs an arbitrary transform — the escape hatch when
// the user wants something the sugar verbs don't cover, or wants
// to drive a future transform from the CLI immediately without
// waiting for sugar.
//
//   iosuite transform <name> [--input PATH] [--output PATH] [--params JSON]
//
// Positional first arg can also be the input path.
func cmdTransform(args []string) error {
	if len(args) == 0 {
		fmt.Println(`Usage: iosuite transform <name> [path] [flags]

Run any registered ffmpeg-serve transform. Sugar verbs (compress,
convert, reframe, normalize) cover the common cases without the
--params dance.

Flags:
  -i, --input string    Input file path (or pass as positional)
  -o, --output string   Output file path (auto-derived if omitted)
      --params string   Transform-specific params as JSON (default "{}")
      --runtime string  Override ffmpeg-serve binary path`)
		return nil
	}
	name := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("transform "+name, flag.ExitOnError)
	var (
		input      = fs.String("input", "", "Input file path")
		inputShort = fs.String("i", "", "(alias for --input)")
		output     = fs.String("output", "", "Output file path (auto-derived if omitted)")
		outShort   = fs.String("o", "", "(alias for --output)")
		paramsJSON = fs.String("params", "{}", "Transform-specific params as a JSON object")
		runtimeBin = fs.String("runtime", "", "Override path to the ffmpeg-serve binary")
	)
	pos := parseInterspersed(fs, rest)
	in := first(*input, *inputShort)
	if in == "" {
		in = pos
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(*paramsJSON), &params); err != nil {
		return fmt.Errorf("--params: %w", err)
	}

	return transform.Run(context.Background(), transform.Options{
		Name:       name,
		Input:      in,
		Output:     first(*output, *outShort),
		Params:     params,
		RuntimeBin: *runtimeBin,
	})
}

// cmdSugar implements the mime-aware sugar verbs (compress, convert,
// reframe, normalize). Each verb's flag surface is small + verb-
// specific, so we map flags → params here rather than sharing one
// flag set.
func cmdSugar(verb string, args []string) error {
	fs := flag.NewFlagSet(verb, flag.ExitOnError)
	var (
		input      = fs.String("input", "", "Input file path")
		inputShort = fs.String("i", "", "(alias for --input)")
		output     = fs.String("output", "", "Output file path (auto-derived if omitted)")
		outShort   = fs.String("o", "", "(alias for --output)")
		runtimeBin = fs.String("runtime", "", "Override path to the ffmpeg-serve binary")
	)

	// Verb-specific flags that map into the params bag.
	var (
		// compress
		target   *string
		sizeMB   *float64
		quality  *int
		bitrate  *int
		fmtFlag  *string
		// reframe
		toAspect *string
		fit      *string
		// convert
		toFormat *string
		// normalize
		targetLUFS *float64
		lra        *float64
		truePeak   *float64
	)
	switch verb {
	case "compress":
		target = fs.String("target", "", "Preset: discord (10 MB) | whatsapp (16 MB) | x | twitter")
		sizeMB = fs.Float64("size-mb", 0, "Video target file size in MB (overrides --target)")
		quality = fs.Int("quality", 0, "Image quality 1-100 (default 75)")
		bitrate = fs.Int("bitrate-kbps", 0, "Audio bitrate kbps 32-320 (default 128)")
		fmtFlag = fs.String("format", "", "Output container format")
	case "reframe":
		toAspect = fs.String("to", "", "Target aspect ratio, \"W:H\" — e.g., 9:16, 1:1, 16:9 (required)")
		fit = fs.String("fit", "", "blur-pad (default) | letterbox | crop | stretch")
	case "convert":
		toFormat = fs.String("to", "", "Target format — e.g., webp, mp4, gif, opus (required)")
	case "normalize":
		targetLUFS = fs.Float64("target-lufs", 0, "Integrated loudness target, LUFS (default -16)")
		lra = fs.Float64("lra", 0, "Loudness range, dB (default 11)")
		truePeak = fs.Float64("true-peak", 0, "True-peak ceiling, dBTP (default -1.5)")
		fmtFlag = fs.String("format", "", "Output container format")
	}

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(),
			"Usage: iosuite %s [flags] [path]\n\n", verb)
		switch verb {
		case "compress":
			fmt.Fprintln(fs.Output(),
				"Shrink image / video / audio. Mime-aware: same verb, three behaviours.")
		case "convert":
			fmt.Fprintln(fs.Output(),
				"Change format. Image / video / audio; --to is required.")
		case "reframe":
			fmt.Fprintln(fs.Output(),
				"Change aspect ratio of image or video. --to is required (e.g. 9:16).")
		case "normalize":
			fmt.Fprintln(fs.Output(),
				"EBU R128 audio loudness normalization. Audio or audio-track-of-video.")
		}
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	pos := parseInterspersed(fs, args)

	in := first(*input, *inputShort)
	if in == "" {
		in = pos
	}
	if in == "" {
		return fmt.Errorf("%s: input file is required (positional or --input)", verb)
	}

	params := map[string]any{}
	switch verb {
	case "compress":
		if *target != "" {
			params["target"] = *target
		}
		if *sizeMB > 0 {
			params["size_mb"] = *sizeMB
		}
		if *quality > 0 {
			params["quality"] = *quality
		}
		if *bitrate > 0 {
			params["bitrate_kbps"] = *bitrate
		}
		if *fmtFlag != "" {
			params["format"] = *fmtFlag
		}
	case "reframe":
		if *toAspect == "" {
			return fmt.Errorf("reframe: --to is required (e.g. --to=9:16)")
		}
		params["to"] = *toAspect
		if *fit != "" {
			params["fit"] = *fit
		}
	case "convert":
		if *toFormat == "" {
			return fmt.Errorf("convert: --to is required (e.g. --to=webp)")
		}
		params["to"] = *toFormat
	case "normalize":
		if *targetLUFS != 0 {
			params["target_lufs"] = *targetLUFS
		}
		if *lra != 0 {
			params["lra"] = *lra
		}
		if *truePeak != 0 {
			params["true_peak"] = *truePeak
		}
		if *fmtFlag != "" {
			params["format"] = *fmtFlag
		}
	}

	return transform.Run(context.Background(), transform.Options{
		Name:       verb,
		Input:      in,
		Output:     first(*output, *outShort),
		Params:     params,
		RuntimeBin: *runtimeBin,
	})
}

// parseInterspersed handles a positional input path appearing BEFORE
// the flags ("iosuite reframe sample.jpg --to=9:16"). Stdlib flag
// stops at the first non-flag, so we parse twice: once to consume any
// leading flags, then if the remainder starts with a non-flag, peel
// it off as the positional and re-parse the trailing flags.
//
// Returns the positional (or "") + nil error iff parsing succeeded.
// fs.ExitOnError is honoured by both Parse calls — on a real flag
// error the runtime exits before we return.
func parseInterspersed(fs *flag.FlagSet, args []string) string {
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		return ""
	}
	positional := rest[0]
	if len(rest) > 1 {
		_ = fs.Parse(rest[1:])
	}
	return positional
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
