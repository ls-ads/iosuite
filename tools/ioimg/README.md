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
| `--provider` | `-p` | `local` | Upscale provider (`local`, `replicate`, `runpod`) |
| `--api-key` | `-k` | | API key for remote providers |
| `--model` | `-m` | `real-esrgan` | Upscale model |
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
ioimg upscale -i photos/ -f png -p runpod
```

#### Supported Input Formats

Only `.jpg`, `.jpeg`, and `.png` files are processed. All other files are silently skipped.

#### Directory Processing

By default, only top-level files in a directory are processed. Use `--recursive` to include subdirectories:

```bash
# Top-level only (default)
ioimg upscale -i photos/ -p runpod

# Include subdirectories
ioimg upscale -i photos/ -r -p runpod
```

#### Resume / Overwrite

By default, files whose output already exists are **skipped**, making the command **idempotent** — running it multiple times with the same arguments is safe and produces the same result. This also lets you resume an interrupted batch without reprocessing completed files:

```bash
# First run processes all 100 images; interrupted at image 60
ioimg upscale -i photos/ -p runpod

# Second run picks up where you left off (skips the 60 already done)
ioimg upscale -i photos/ -p runpod

# Force reprocessing of everything
ioimg upscale -i photos/ -p runpod --overwrite
```

#### Error Handling

Batch processing **stops on the first error** by default. There is no automatic retry — failures are typically caused by the input file itself (e.g. corrupt or unsupported content), so retrying the same file would produce the same result.

Because the command is idempotent, you can fix or remove the problematic file and re-run the same command to continue where you left off.

To process all files regardless of individual failures, use `--continue-on-error`:

```bash
# Stop on first error (default)
ioimg upscale -i photos/ -p runpod

# Keep going even if some files fail
ioimg upscale -i photos/ -p runpod --continue-on-error
```

#### Examples

```bash
# Upscale a single image locally
ioimg upscale -i photo.jpg

# Upscale with RunPod, output to specific path
ioimg upscale -i photo.jpg -o upscaled.png -f png -p runpod

# Batch upscale a directory
ioimg upscale -i photos/ -p runpod

# Batch upscale recursively with JSON output
ioimg upscale -i photos/ -r -p runpod --json
```

#### Metrics

After processing, a summary table is displayed:

```
┌──────────────┬──────────────┐
│    METRIC    │    VALUE     │
├──────────────┼──────────────┤
│ Total Files  │ 6            │
│ Skipped      │ 2            │
│ Succeeded    │ 3            │
│ Failed       │ 1            │
│ Total Time   │ 42.310s      │
│ Billed Time  │ 18.600s      │
│ Avg Time/Img │ 3.720s       │
│ Total Cost   │ $0.0038      │
│ Avg Cost/Img │ $0.0008      │
│ Input Size   │ 2.45 MB      │
│ Output Size  │ 38.12 MB     │
└──────────────┴──────────────┘
```

| Metric | Description |
|--------|-------------|
| **Total Files** | Number of images found to process |
| **Skipped** | Images skipped because output already exists (use `--overwrite` to reprocess) |
| **Succeeded** | Images upscaled successfully |
| **Failed** | Images that failed (errors listed below the table) |
| **Total Time** | Wall-clock time from start to finish, including network overhead |
| **Billed Time** | Sum of GPU execution time reported by the provider (what you pay for) |
| **Avg Time/Img** | Average billed time per successful image (`Billed Time / Succeeded`) |
| **Total Cost** | Estimated total cost based on billed time × provider rate |
| **Avg Cost/Img** | Average cost per successful image (`Total Cost / Succeeded`) |
| **Input Size** | Combined size of all input files |
| **Output Size** | Combined size of all output files |

Use `--json` to get the same metrics in machine-readable JSON format.

### `ioimg upscale init`

Provision cloud infrastructure for upscaling.

```bash
# Initialize a RunPod endpoint
ioimg upscale init -p runpod
```

### `ioimg upscale model list`

List available upscale models.

### `ioimg upscale provider list`

List available upscale providers and their API key requirements.
