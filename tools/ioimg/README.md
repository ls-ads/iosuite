# ioimg

Image processing tool from the iosuite toolchain.

## Commands

### `ioimg upscale`

Upscale images using local or remote GPU providers.

```
ioimg upscale -i <input> [flags]
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--input` | `-i` | | Input image or directory (required) |
| `--output` | `-o` | `<input>_out` | Output path (file or directory) |
| `--format` | `-f` | match input | Output format: `jpg` or `png` |
| `--recursive` | `-r` | `false` | Recursively process subdirectories |
| `--overwrite` | | `false` | Reprocess all files even if output already exists |
| `--continue-on-error` | `-c` | `false` | Continue processing remaining files after a failure |
| `--provider` | `-p` | (required for start/stop) | Execution provider (`local_cpu`, `local_gpu`, `runpod`) |
| `--api-key` | `-k` | | API key for remote providers |
| `--model` | `-m` | (required for start/stop) | Upscale model (or `ffmpeg`) |
| `--volume` | | | RunPod Network Volume ID to use for processing |
| `--gpu-id` | | `NVIDIA RTX A4000` | Requested GPU type for RunPod |
| `--keep-failed` | | `false` | Keep the created volume if the job fails |
| `--json` | | `false` | Output metrics as JSON |

#### Output Path

When `-o` is omitted, the output path is derived automatically:

- **Single file**: `photo.jpg` → `photo_out.jpg`
- **Directory**: `photos/` → `photos_out/`

Output directories are created automatically if they don't exist.

#### Output Format

By default, the output format matches the input file extension:

- `.jpg` / `.jpeg` → JPEG output
- `.png` → PNG output

Use `--format` to override for all files:

```bash
# Convert all outputs to PNG regardless of input
ioimg upscale \
  -i photos/ \
  -f png \
  -p runpod
```

#### Supported Input Formats

Only `.jpg`, `.jpeg`, and `.png` files are processed. All other files are silently skipped.

#### Directory Processing

By default, only top-level files in a directory are processed. Use `--recursive` to include subdirectories:

```bash
# Top-level only (default)
ioimg upscale -i photos/ -p runpod

# Include subdirectories
ioimg upscale \
  -i photos/ \
  -r \
  -p runpod
```

#### Resume / Overwrite

By default, files whose output already exists are **skipped**, making the command **idempotent** — running it multiple times with the same arguments is safe and produces the same result. This also lets you resume an interrupted batch without reprocessing completed files:

```bash
# First run processes all 100 images; interrupted at image 60
ioimg upscale -i photos/ -p runpod

# Second run picks up where you left off (skips the 60 already done)
ioimg upscale -i photos/ -p runpod

# Force reprocessing of everything
ioimg upscale \
  -i photos/ \
  -p runpod \
  --overwrite
```

#### Error Handling

Batch processing **stops on the first error** by default. There is no automatic retry — failures are typically caused by the input file itself (e.g. corrupt or unsupported content), so retrying the same file would produce the same result.

Because the command is idempotent, you can fix or remove the problematic file and re-run the same command to continue where you left off.

To process all files regardless of individual failures, use `--continue-on-error`:

```bash
# Stop on first error (default)
ioimg upscale -i photos/ -p runpod

# Keep going even if some files fail
ioimg upscale \
  -i photos/ \
  -p runpod \
  --continue-on-error
```

#### Examples

```bash
# Upscale a single image locally
ioimg upscale -i photo.jpg

# Upscale with RunPod, output to specific path
ioimg upscale \
  -i photo.jpg \
  -o upscaled.png \
  -f png \
  -p runpod

# Batch upscale a directory
ioimg upscale -i photos/ -p runpod

# Batch upscale recursively with JSON output
ioimg upscale \
  -i photos/ \
  -r \
  -p runpod \
  --json
```

#### Metrics

After processing, a summary table is displayed:

```
┌─────────────────┬──────────────┐
│     METRIC      │    VALUE     │
├─────────────────┼──────────────┤
│ Total Files     │ 6            │
│ Skipped         │ 2            │
│ Succeeded       │ 3            │
│ Failed          │ 1            │
│ Total Time      │ 42.310s      │
│ Processing Time │ 18.600s      │
│ Avg Time/Img    │ 3.720s       │
│ Total Cost      │ $0.0038      │
│ Avg Cost/Img    │ $0.0008      │
│ Input Size      │ 2.45 MB      │
│ Output Size     │ 38.12 MB     │
└─────────────────┴──────────────┘
```

| Metric | Description |
|--------|-------------|
| **Total Files** | Number of images found to process |
| **Skipped** | Images skipped because output already exists (use `--overwrite` to reprocess) |
| **Succeeded** | Images upscaled successfully |
| **Failed** | Images that failed (errors listed below the table) |
| **Total Time** | Wall-clock time from start to finish, including network overhead |
| **Processing Time** | Sum of GPU execution time reported by the provider |
| **Avg Time/Img** | Average processing time per successful image (`Processing Time / Succeeded`) |
| **Total Cost** | Estimated total cost — based on wall time for active endpoints, or execution time for flex |
| **Avg Cost/Img** | Average cost per successful image (`Total Cost / Succeeded`) |
| **Input Size** | Combined size of all input files |
| **Output Size** | Combined size of all output files |

Use `--json` to get the same metrics in machine-readable JSON format.

### `ioimg start`

Provision cloud infrastructure for the selected model. Currently supports RunPod. **Provider and model flags are required.**

```bash
# Start RunPod infrastructure for ffmpeg
ioimg start -p runpod -m ffmpeg

# Start always-active infrastructure for real-esrgan in a specific data center
ioimg start \
  -m real-esrgan \
  -p runpod \
  --active \
  --data-center US-TX-3 \
  --gpu "NVIDIA RTX A5000"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` / `-p` | (required) | Provider to start (`runpod` is the main target for infrastructure) |
| `--api-key` / `-k` | | API key (or set `RUNPOD_API_KEY`) |
| `--model` / `-m` | (required) | Model to provision |
| `--active` | `false` | Keep at least one worker always running (`workersMin=1`) |
| `--data-center` | `EU-RO-1` | Specific RunPod data center ID(s) (comma-separated) |
| `--gpu` | | Specific RunPod GPU type (e.g. `NVIDIA RTX A4000`) |
| `--volume-size` | | Size in GB for a new Network Volume to provision (min 10) |

> [!IMPORTANT]
> **Authentication**: `RUNPOD_API_KEY` is required for all RunPod operations. For Network Volume S3 access, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are **strictly required**.

### `ioimg stop`

Stop running processes or tear down cloud resources. **Provider and model flags are required.**

```bash
# Stop local FFmpeg processes (terminates background workers)
ioimg stop -p local_gpu -m ffmpeg

# Tear down RunPod endpoints for a specific model
ioimg stop -p runpod -m real-esrgan
```

| Flag | Description |
|------|-------------|
| `--provider` / `-p` | Provider to stop (required) |
| `--model` / `-m` | Model name to stop (required) |
| `--yes` / `-y` | Skip confirmation prompt for resource deletion |

### `ioimg upscale start`

If an endpoint for the model already exists, the command reports it and exits without creating a duplicate.

**Active vs Flex pricing**: Active endpoints have a lower per-second rate but you pay for the always-on worker even when idle. Flex endpoints scale to zero but have a higher per-second rate and cold-start latency.

For **Flex workers**, billing covers three periods:
1. **Start time**: Worker initialization and model loading (cold starts).
2. **Execution time**: The actual processing of the request.
3. **Idle time**: Time the worker stays active after completing a request before scaling down.

All three are billed per second (rounded up to the next full second). The `Processing Time` reported by `ioimg` corresponds to the **Execution time** portion. While this is the dominant cost for sequential jobs on a warm worker, the actual amount billed by RunPod may be slightly higher if cold starts or idle periods are incurred.

The `delayTime` field from RunPod captures platform-side queue and cold-start wait, which is a close approximation of the billed start time. In practice, `executionTime` is the core billing metric for flex; it just slightly underestimates total cost when cold starts are involved.

#### Supported GPUs

By default, endpoints are configured with a prioritized list of efficient 16GB GPUs:

1. NVIDIA RTX A4000 (Default)
2. NVIDIA RTX A4500
3. NVIDIA RTX 4000 Ada Generation
4. NVIDIA RTX 4000 SFF Ada Generation
5. NVIDIA RTX 2000 Ada Generation
6. NVIDIA RTX A2000

You can override this and specify any valid RunPod GPU type using the `--gpu` flag. Valid options include `NVIDIA GeForce RTX 4090`, `NVIDIA A100-SXM4-80GB`, `NVIDIA H100 PCIe`, `NVIDIA B200`, etc.

List available GPUs for a specific provider. This is useful for finding the correct GPU name to use with `start --gpu`.

```bash
# List all available GPUs for RunPod
ioimg upscale \
  provider \
  gpus \
  runpod
```

### `ioimg runpod volume`

Manage RunPod network volumes for large media handling. The serverless workflow automatically uses these volumes to avoid base64 overhead for large files.

- **Direct Upload/Download**: Files are uploaded to the volume via S3, processed locally by the worker in `/workspace`, and then downloaded.
- **Idempotency**: Volumes are named based on the project/task to ensure consistency across retries.

```bash
# Create a 100GB volume in EU-RO-1
ioimg runpod volume create \
  --name my-media \
  --size 100 \
  --data-center EU-RO-1

# List all volumes
ioimg runpod volume list

# Delete a volume
ioimg runpod volume delete --id <volume-id>
```

### Transformation Verbs

Atomic image transformations and bridge commands using FFmpeg.

- **Geometric**: `scale`, `crop`, `rotate`, `flip`, `pad`.
- **Visual**: `brighten`, `contrast`, `saturate`, `denoise`, `sharpen`.
- **Bridge**: `combine` (images to video).
- **Control**: `pipeline` (chained execution).

All transformation verbs support the global `--provider` flag. While infrastructure commands (`start`, `stop`) require explicit flags, transformation verbs will default to `local_gpu` and `ffmpeg` if the flags are omitted for ease of use.

### `ioimg pipeline`

Run multiple transformations chained together in a single execution pass. This is highly optimized for GPU and minimizes data transfer on RunPod.

```bash
# Locally using GPU (default)
ioimg pipeline \
  -i photo.jpg \
  -o photo_ready.jpg \
  --ops "scale=1920x1080,brighten=0.05,contrast=10"

# Remotely on RunPod
ioimg pipeline \
  -i photo.jpg \
  -o photo_ready.jpg \
  -p runpod \
  --ops "rotate=90,scale=1080x1920"
```

| Flag | Description |
|------|-------------|
| `--ops` | Comma-separated operations (e.g. `scale=1280x720,brighten=0.1`) |

Supported ops: `scale=WxH`, `crop=WxHxXxY`, `rotate=DEG`, `flip=v/h`, `brighten=LEVEL`, `contrast=LEVEL`, `saturate=LEVEL`, `denoise=PRESET`, `sharpen=AMOUNT`.

### Execution Providers

Iosuite is designed for high-performance by default.

#### `local_gpu` (Default)
Uses NVIDIA hardware acceleration (`-hwaccel cuda`) and optimized filters (like `scale_npp`) for zero-copy data flow. Requires an NVIDIA GPU and drivers.

#### `local_cpu`
Standard software processing using system CPU.

#### `runpod`
Executes tasks on RunPod serverless endpoints. Requires an API key and infrastructure initialization via `start`.

```bash
# Scale an image
ioimg scale \
  -i photo.jpg \
  -o photo_scaled.jpg \
  --width 1920 \
  --height 1080

# Adjust contrast (-100 to 100)
ioimg contrast -i photo.jpg -o photo_crisp.jpg --level 20

# Flip an image (h or v)
ioimg flip -i photo.jpg -o photo_flipped.jpg --axis v

# Pad an image to aspect ratio
ioimg pad -i photo.jpg -o photo_padded.jpg --aspect 16:9

# Adjust saturation (0.0 to 3.0)
ioimg saturate -i photo.jpg -o photo_vibrant.jpg --level 1.5

# Denoise an image (presets: weak, med, strong)
ioimg denoise -i photo.jpg -o photo_clean.jpg --preset strong

# Sharpen an image (0.0 to 5.0)
ioimg sharpen -i photo.jpg -o photo_sharp.jpg --amount 2.0

# Combine frames into a video
ioimg combine \
  -i ./frames/frame_%05d.png \
  -o output.mp4 \
  --fps 30
```

### `ioimg list`

Display a feature matrix of all commands and their supported providers (`local_cpu`, `local_gpu`, `runpod`, `replicate`).

For full cross-platform installation instructions, including shell autocompletion for Bash, Zsh, Fish, and PowerShell, please refer to the [Root Installation Guide](../../README.md#️-installation--shell-completion).

### `ioimg upscale model list`

List available upscale models.

### `ioimg upscale provider list`

List available upscale providers and their API key requirements.
