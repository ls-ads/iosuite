# iosuite

The user-facing CLI for the iosuite ecosystem. Drives image (and future
video / audio) processing through the same `real-esrgan-serve`
ecosystem the [iosuite.io](https://iosuite.io) web service uses
internally — local GPU, RunPod serverless, or a remote `serve` daemon,
all through one command.

```
$ iosuite upscale cat.jpg
→ cat.jpg
  ✓ cat_4x.jpg (4096×3072)
```

That's the whole experience. Auto-derived output name, sticky config,
no flags required.

## Install

```bash
go install iosuite.io/cmd/iosuite@latest
# or download a release tarball
curl -sSL https://raw.githubusercontent.com/ls-ads/iosuite/main/scripts/install.sh | bash
```

The CLI subprocesses to `real-esrgan-serve` for the actual GPU work.
`iosuite doctor` will tell you whether it's on your PATH and how to
install it if not.

## Quick start

```bash
# Probe the host: confirm real-esrgan-serve, Python, GPU, etc.
iosuite doctor

# One-shot upscale (local subprocess to real-esrgan-serve)
iosuite upscale photo.jpg

# A whole directory at once
iosuite upscale ~/photos -o ~/photos_4x
```

## Subcommands

| Command                | Purpose                                                       |
|------------------------|---------------------------------------------------------------|
| `iosuite upscale`      | Run Real-ESRGAN inference. Subprocesses `real-esrgan-serve`. |
| `iosuite doctor`       | Diagnose the host: PATH, Python, ORT, GPU, auth keys.        |
| `iosuite fetch-model`  | Pull a verified model artefact from `real-esrgan-serve`.     |
| `iosuite version`      | Print version + build commit.                                |

Run `iosuite <cmd> --help` for the full flag surface.

## Config

`~/.config/iosuite/config.toml`:

```toml
[default]
provider = "local"
model    = "realesrgan-x4plus"

[runpod]
# api_key / endpoint_id fall back to RUNPOD_API_KEY / RUNPOD_ENDPOINT_ID
# in env if you'd rather not commit them to a config file.
```

Precedence: command-line flag > environment variable > config file >
built-in defaults.

## What's coming

`iosuite serve` (long-lived daemon for hot-path), `iosuite benchmark`
(throughput + cost-per-job report), and `iosuite endpoint deploy`
(provision a RunPod / vast.ai / Modal endpoint with one command). All
documented in [`ARCHITECTURE.md`](./ARCHITECTURE.md).

## Migration from the legacy CLI

The pre-rebuild iosuite (CGO + Python/Node bindings, separate
`ioimg`/`iovid` binaries) is preserved on the `legacy-cgo` branch and
the `legacy-final-2026-04-30` tag. The new shape — single binary, no
CGO, no embedded weights — drops the install matrix and license-tangled
NVIDIA containers; that's why the rewrite happened.

## License

Apache-2.0. See [`LICENSE`](./LICENSE) for the full text and
`ARCHITECTURE.md` for the design rationale.
