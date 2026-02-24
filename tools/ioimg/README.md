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
│ Succeeded    │ 5            │
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
