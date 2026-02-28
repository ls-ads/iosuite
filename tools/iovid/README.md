# iovid

`iovid` is a high-performance CLI tool for video transformations, built on top of FFmpeg. It provides atomic verbs for geometric and visual adjustments, as well as bridge commands to move data between images and videos.

## Features

- **Geometric Transformations**: `scale`, `crop`, `rotate`, `flip`, `pad`.
- **Visual Adjustments**: `brighten`, `contrast`, `saturate`, `denoise`, `sharpen`.
- **Temporal Operations**: `trim`, `fps`, `mute`, `speed`.
- **Bridge Commands**: `extract-frames` (video to images), `extract-audio` (video to audio), `stack` (side-by-side comparison).
- **Capability Matrix**: `list` (show command-provider support).

## Usage

### General Flags
- `-i`, `--input`: Input video file.
- `-o`, `--output`: Output file or directory path.
- `-p`, `--provider`: Execution provider (`local_cpu`, `local_gpu`, `runpod`). (required for start/stop)
- `-k`, `--api-key`: API key for remote execution.
- `-m`, `--model`: Execution model. (required for start/stop)
- `--volume`: RunPod Network Volume ID to use for processing.
- `--volume-size`: Size in GB for a new Network Volume to provision during `start`.
- `--gpu-id`: Requested GPU type for RunPod (e.g. `NVIDIA RTX A4000`).
- `--keep-failed`: Keep the created volume if the job fails.

### Optimized Providers

- **`local_gpu` (Default)**: Leverages FFmpeg hardware acceleration (`-hwaccel cuda`) and zero-copy filters.
- **`local_cpu`**: Standard CPU processing.
- **`runpod`**: Remote execution on RunPod serverless.

### `iovid pipeline`

Chain multiple video transformations in a single pass to save time and reduce I/O.

```bash
iovid pipeline -i input.mp4 -o output.mp4 --ops "trim=00:00:10-00:00:20,scale=1280x720,brighten=0.1"
```

| Flag | Description |
|------|-------------|
| `--ops` | Comma-separated operations |

Supported ops: `scale=WxH`, `crop=WxHxXxY`, `rotate=DEG`, `flip=v/h`, `brighten=LEVEL`, `contrast=LEVEL`, `saturate=LEVEL`, `denoise=PRESET`, `sharpen=AMOUNT`, `trim=START-END`, `fps=RATE`, `mute`, `speed=MULT`.

### Geometric Transformations
```bash
# Scale video
iovid scale -i input.mp4 -o output.mp4 --width 1920 --height 1080

# Crop video
iovid crop -i input.mp4 -o output.mp4 --w 1280 --h 720 --x 100 --y 100

# Rotate video
iovid rotate -i input.mp4 -o output.mp4 --degrees 90
```

### Visual Adjustments
```bash
# Adjust brightness (-1.0 to 1.0)
iovid brighten -i input.mp4 -o output.mp4 --level 0.2

# Flip video (h or v)
iovid flip -i input.mp4 -o output.mp4 --axis h

# Pad video to aspect ratio
iovid pad -i input.mp4 -o output.mp4 --aspect 1:1

# Adjust saturation (0 to 3)
iovid saturate -i input.mp4 -o output.mp4 --level 1.5

# Denoise video (presets: weak, med, strong)
iovid denoise -i input.mp4 -o output.mp4 --preset med

# Sharpen video (0 to 5)
iovid sharpen -i input.mp4 -o output.mp4 --amount 1.2
```

For full cross-platform installation instructions, including shell autocompletion for Bash, Zsh, Fish, and PowerShell, please refer to the [Root Installation Guide](../../README.md#️-installation--shell-completion).

### Temporal & Stream Operations
```bash
# Trim video
iovid trim -i input.mp4 -o output.mp4 --start 00:00:10 --end 00:00:20

# Change frame rate
iovid fps -i input.mp4 -o output.mp4 --rate 24

# Remove audio
iovid mute -i input.mp4 -o output.mp4

# Change video speed (multiplier)
iovid speed -i input.mp4 -o output.mp4 --multiplier 2.0
```

### Bridge Commands
```bash
# Extract frames from video
iovid extract-frames -i input.mp4 -o ./frames/

# Extract audio from video
iovid extract-audio -i input.mp4 -o audio.mp3

# Stack two videos side-by-side
iovid stack -i original.mp4 -i2 modified.mp4 -o comparison.mp4 --axis h
```

Provision cloud infrastructure for the selected model. Currently supports RunPod. **Provider and model flags are required.**

```bash
# Start RunPod infrastructure for ffmpeg
iovid start -p runpod -m ffmpeg
```

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` / `-p` | (required) | Provider to start |
| `--model` / `-m` | (required) | Model name |
| `--data-center` | `EU-RO-1` | Data center ID(s) |
| `--active` | `false` | Keep at least one worker always running |
| `--volume-size` | | Size in GB for a new Network Volume |

### RunPod Network Volumes

For large videos, `iovid` automatically leverages RunPod Network Volumes to avoid the overhead of base64 encoding.

- **Direct Upload**: The input video is uploaded to `/workspace` on the worker via S3.
- **Local Processing**: FFmpeg processes the file directly on the volume's high-speed storage.
- **Direct Download**: The resulting video is downloaded back to your local machine.

Specify a volume using `--volume <id>` or have one created during `start` with `--volume-size <GB>`.

Stop running processes or tear down cloud resources. **Provider and model flags are required.**

```bash
# Stop local FFmpeg processes
iovid stop -p local_gpu -m ffmpeg

# Tear down RunPod endpoints
iovid stop -p runpod -m ffmpeg
```

## Requirements
- `ffmpeg` must be installed and available in your PATH.
- For `local_gpu`, NVIDIA GPU drivers and a CUDA-compatible FFmpeg build are required.
