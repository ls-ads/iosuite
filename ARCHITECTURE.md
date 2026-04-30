# iosuite — architecture

`iosuite` is the user-facing CLI for the iosuite ecosystem. It's a thin
Go orchestrator that subprocesses model-specific binaries (`real-esrgan-serve`
first; future `whisper-serve`, `stable-diffusion-serve`, etc.) — same shape
as `docker` shells out to `dockerd`, or `gh` shells out to `git`.

The goal is "type one thing, get the right answer" without paying CGO,
nvidia-tangled-licensing, or "you also need to install Python, ORT, CUDA,
cuDNN, TensorRT" friction at install time. iosuite checks for what it
needs, fetches model artefacts on demand, and runs.

## Goals

1. **Single binary, no dependencies at install time.** `go install
   iosuite.io/cmd/iosuite` (or download a release tarball) — that's it.
   Subprocess targets (`real-esrgan-serve`, future tools) come from
   GitHub Releases when needed.
2. **Cross-platform from one source.** CGO=0 across the board. Linux,
   macOS, Windows from the same `go build`. No platform branches in code.
3. **Same UX everywhere.** Local GPU, RunPod serverless, or a remote
   `serve` daemon — all driven by the same `iosuite upscale photo.jpg`,
   only the provider config differs.
4. **Opinionated zero-flag happy path.** First-time users type
   `iosuite upscale cat.jpg` and get `cat_4x.jpg` next to it. Sticky
   config in `~/.config/iosuite/config.toml`. Power users override via
   flags or env.
5. **Self-host as a first-class deploy target.** The exact runtime
   `iosuite serve` exposes is what iosuite.io's backend talks to. Ship
   improvements to the CLI, both groups benefit.

## Tool surface (first cut)

| Subcommand           | What it does                                                          |
|----------------------|------------------------------------------------------------------------|
| `iosuite upscale`    | Subprocess to `real-esrgan-serve upscale`. Local GPU or remote pick. |
| `iosuite doctor`     | Probe the host: Go binary on PATH? Python+ORT? GPU? Auth keys?       |
| `iosuite fetch-model`| Forward to `real-esrgan-serve fetch-model` (verified GH-Releases).   |
| `iosuite version`    | Print version + build commit.                                        |

Coming in subsequent rounds (this doc is for round 1):

| Subcommand                | Notes                                                                |
|---------------------------|----------------------------------------------------------------------|
| `iosuite serve`           | Long-lived HTTP daemon for hot-path / iosuite.io backend wiring.     |
| `iosuite benchmark`       | Same throughput / p50 / p95 / p99 / cost-per-job shape as iosuite.io.|
| `iosuite endpoint deploy` | RunPod (and future vast.ai / Modal) endpoint provisioning.           |

## Provider abstraction

`internal/provider` is the routing layer. Each provider implements one
method that turns "an upscale request" into "a result file."

| Provider | Backend                                                              |
|----------|----------------------------------------------------------------------|
| `local`  | `real-esrgan-serve upscale ...` as a subprocess on this host.        |
| `runpod` | HTTP POST to a RunPod serverless endpoint (the iosuite.io path).     |
| `serve`  | HTTP POST to a `real-esrgan-serve serve` daemon at a configured URL. |

Round 1 ships `local` only; the interface is in place so rounds 2+
can add the other two without restructuring callers.

## Subprocess contract with `real-esrgan-serve`

iosuite never imports `real-esrgan-serve`'s Go packages. The contract
is the documented CLI surface:

- **Stable subcommand names + flag shapes** — semver-bound by
  `real-esrgan-serve`.
- **JSON events on stdout** when invoked with `--json-events`. iosuite
  parses these to render progress bars, capture errors, and feed
  benchmark output.
- **Documented exit codes**: `0` = success, `1` = user error,
  `2` = runtime error, `3` = environment error.
- **Stderr is human-readable.** iosuite passes it through to the user
  unmodified when not in `--json-events` mode.

The binary is located via `os.LookPath("real-esrgan-serve")`. If
missing, `iosuite doctor` reports "real-esrgan-serve not on PATH" with
the exact command to fix it.

## Sticky config

`~/.config/iosuite/config.toml` holds defaults so users don't repeat
flags. Layout:

```toml
[default]
provider   = "local"          # local | runpod | serve
output_dir = ""               # empty = alongside input
model      = "realesrgan-x4plus"

[runpod]
api_key     = ""              # falls back to RUNPOD_API_KEY env
endpoint_id = ""              # falls back to RUNPOD_ENDPOINT_ID env

[serve]
url = "http://127.0.0.1:8311"
```

Flags > env > config file > built-in defaults. The same precedence
pattern as `kubectl`, `docker`, `gh`.

## Distribution

- `go install iosuite.io/cmd/iosuite@latest` for Go users.
- GitHub Releases tarballs (one per platform) for everyone else, fetched
  by `scripts/install.sh`.
- The `real-esrgan-serve` binary is fetched on demand by `iosuite
  fetch-model` (or any subcommand that needs it). Same release-asset
  channel.

## What this repo deliberately doesn't ship

- **CGO bridges or Python/Node bindings.** The legacy shape (preserved
  on the `legacy-cgo` branch and the `legacy-final-2026-04-30` tag)
  carried these. Drops the build matrix from "5 platforms × 3 host
  Python versions × 2 architectures" down to "5 platforms, one binary."
- **An embedded model.** Model artefacts live in
  `real-esrgan-serve`'s GH Releases with SHA-256 manifests; we never
  vendor them.
- **A web UI.** The embedded SvelteKit UI is a future round; not on the
  critical path for the iosuite.io migration.

## Repo layout (round 1)

```
iosuite/
├── ARCHITECTURE.md          this file
├── README.md
├── LICENSE                  Apache-2.0
├── Makefile
├── go.mod
├── cmd/iosuite/main.go      cobra entry
└── internal/
    ├── config/              ~/.config/iosuite/config.toml load/save
    ├── runtime/             find the real-esrgan-serve binary on PATH
    ├── upscale/             `iosuite upscale` subcommand
    ├── doctor/              `iosuite doctor` subcommand
    └── version/             build-time version info
```
