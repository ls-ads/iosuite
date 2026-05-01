# iosuite

User-facing CLI for the iosuite ecosystem. One Go binary that
upscales an image locally on your GPU, runs a long-lived daemon for
hot-path workloads, manages serverless endpoints on RunPod, and
benchmarks them against the workload each tool publishes.

```
$ iosuite upscale --input cat.jpg
→ cat.jpg
  ✓ cat_4x.jpg (4096×3072)
```

CGO-free, cross-compiles cleanly, single static binary.

## Install

Build from source (Go 1.25+):

```bash
git clone https://github.com/ls-ads/iosuite
cd iosuite
make install
# installs to /usr/local/bin/iosuite
```

Or grab a binary from
[GitHub releases](https://github.com/ls-ads/iosuite/releases) and put
it on your PATH.

For local upscale you also need `real-esrgan-serve` on PATH (or pass
`--runtime <path>`). For RunPod-only flows
(`iosuite serve --provider runpod`, `iosuite endpoint *`) the CLI has
no other dependencies.

## Quick start

```bash
# Probe the host: real-esrgan-serve, Python, RunPod creds.
iosuite doctor

# Single image, local GPU.
iosuite upscale --input photo.jpg
# → photo_4x.jpg

# Tile mode for inputs >1280² (slices, infers per tile, stitches).
iosuite upscale --input large.png --tile

# JSON progress events for piping into other tools.
iosuite upscale --input photo.jpg --json-events
```

Output is always 4× input on each axis (Real-ESRGAN
`realesrgan-x4plus`). Override the model with `--model` once
additional variants ship.

## Subcommands

| Command                       | What it does                                                       |
|-------------------------------|--------------------------------------------------------------------|
| `iosuite upscale`             | One-shot inference. Subprocesses `real-esrgan-serve`.              |
| `iosuite serve`               | Long-lived HTTP daemon (`local` or `runpod` provider).             |
| `iosuite endpoint deploy`     | Create / update a RunPod serverless endpoint from a manifest.      |
| `iosuite endpoint list`       | List endpoints on the configured RunPod account.                   |
| `iosuite endpoint destroy`    | Delete an endpoint by id or name.                                  |
| `iosuite endpoint benchmark`  | Run the tool's published benchmark suite against an endpoint.      |
| `iosuite doctor`              | Diagnose the host: PATH, Python, GPU, RunPod credentials.          |
| `iosuite fetch-model`         | Pull a verified model artefact (forwarded to `real-esrgan-serve`). |
| `iosuite version`             | Print version + build commit.                                      |

`iosuite <cmd> --help` prints the full flag surface for any command.

## Daemon mode

`iosuite serve` exposes a stable JSON API regardless of which backend
runs the inference:

```bash
# Local: spawns a real-esrgan-serve subprocess on :8311.
iosuite serve --provider local --bind 0.0.0.0 --port 8312

# RunPod: forwards each request to api.runpod.ai/v2/<id>/runsync,
# falls back to /status polling for cold queues.
iosuite serve --provider runpod \
  --endpoint-id <id> \
  --runpod-api-key <key>
```

Wire shape (RunPod-compatible — same envelope both providers see):

```
POST /runsync   Content-Type: application/json
{"input": {"images": [{"image_base64": "..."}],
           "tile": true, "output_format": "jpg"}}

→ {"status": "COMPLETED",
   "output": {"outputs": [{"image_base64": "...", "exec_ms": 612}]}}
```

`/upscale` is an alias of `/runsync`. `GET /health` returns
`{"status":"ok"}` once the backend is reachable.

## Endpoint management

Per-tool deploy specs (image tag, container disk, GPU pool map,
FlashBoot default, CUDA pin) live in each `*-serve` repo's
`deploy/runpod.json` manifest. iosuite reads the manifest at deploy
time — adding a new tool is a manifest, not an iosuite release.

```bash
# Deploy / update — idempotent. Resolves the manifest from the
# *-serve repo at the registered stable git tag.
iosuite endpoint deploy --tool real-esrgan --gpu-class rtx-4090

# Pin to a specific *-serve git tag.
iosuite endpoint deploy --tool real-esrgan \
  --version runpod-trt-0.2.2 --gpu-class rtx-4090

# Override defaults from the manifest.
iosuite endpoint deploy --tool real-esrgan --gpu-class rtx-4090 \
  --workers-max 3 --idle-timeout 30 --min-cuda 12.8

# List + destroy.
iosuite endpoint list
iosuite endpoint destroy <id>
iosuite endpoint destroy --name real-esrgan-rtx-4090
```

## Benchmark

Each tool publishes a `deploy/benchmark.json` manifest declaring the
workload (warmup count, measure count, request shape, metrics).
iosuite owns the wire — POST loop, timing, percentile aggregation,
table formatting.

```bash
iosuite endpoint benchmark --tool real-esrgan --endpoint-id <id>
# benchmark: tool=real-esrgan endpoint=<id> warmup=3 measure=10
# manifest:  https://raw.githubusercontent.com/ls-ads/real-esrgan-serve/main/deploy/benchmark.json
#
#   p50_latency_ms   p50 = 19.0
#   p95_latency_ms   p95 = 19.0
#   p99_latency_ms   p99 = 19.0
#   mean_latency_ms  mean = 18.2
```

## Configuration

`~/.config/iosuite/config.toml` (honours `$XDG_CONFIG_HOME`):

```toml
[default]
provider   = "local"             # local | runpod
output_dir = ""                  # empty = alongside input
model      = "realesrgan-x4plus"

[runpod]
api_key     = ""                 # also honours $RUNPOD_API_KEY
endpoint_id = ""                 # also honours $RUNPOD_ENDPOINT_ID
```

Resolution order (highest wins): command-line flag → environment
variable → config file → built-in default.

## Documentation

- Full CLI reference: <https://iosuite.io/cli-docs>
- Architecture and the interface/implementation split:
  [`ARCHITECTURE.md`](./ARCHITECTURE.md)
- Worker module: [real-esrgan-serve](https://github.com/ls-ads/real-esrgan-serve)

## License

Apache-2.0. See [`LICENSE`](./LICENSE).
