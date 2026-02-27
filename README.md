# iosuite

A high-performance, unified suite for image and video processing. Leveraging FFmpeg, local GPU acceleration (NCNN/Vulkan/CUDA), and serverless cloud providers (RunPod/Replicate).

## Architecture

`iosuite` is built on a modular, Go-native core designed for maximum portability and performance.

### Components
- **`iocore`**: The engine. A pure-Go library providing the pipeline logic, hardware acceleration wrappers, and cloud bridge.
- **`ioimg`**: CLI tool for high-performance image processing and upscaling.
- **`iovid`**: CLI tool for comprehensive video transformations and filters.
- **`libiocore`**: A C-shared library bridge allowing the core logic to be imported by Python, Rust, or C++ applications.

---

## Universal Compatibility

`iosuite` is designed to be truly universal, supporting multiple operating systems and CPU architectures.

| OS | Architectures | Accelerated By |
| :--- | :--- | :--- |
| **Linux** | x86_64, ARM64 | NVIDIA CUDA / NVENC |
| **Windows** | x86_64, ARM64 | NVIDIA CUDA / NVENC |
| **macOS** | Apple Silicon, Intel | VideoToolbox (Hardware) |

### Zero-Config Cross-Compilation
The CLI tools (`ioimg`, `iovid`) are written in **pure Go** and avoid CGO dependencies. This allows them to be cross-compiled for any platform with a single command:

```bash
# Build for all supported platforms and architectures
make build-all
```

> [!IMPORTANT]
> **CGO & Shared Libraries**: While the CLI tools are CGO-free, building the `libiocore` shared library (`.so`, `.dll`) **requires CGO** and a host C cross-compiler for the target platform. If you only need the CLI tools, you do not need to worry about C toolchains.

---

## Installation & Setup

Installation is a two-step process: First, install the **CLI tools** themselves; then, use those tools to provision the **processing models** (like FFmpeg).

### 1. Install CLI Tools (`ioimg` & `iovid`)

#### Option A: Download Pre-built Binaries (Recommended)
Download the latest binary for your OS and architecture from the [GitHub Releases](https://github.com/ls-ads/iosuite/releases).
- **Windows**: `ioimg-windows-amd64.exe`
- **Linux**: `ioimg-linux-amd64`
- **macOS (Apple Silicon)**: `ioimg-darwin-arm64`

#### Option B: Build from Source
If you have Go installed (1.25.5+), you can build the tools natively:
```bash
make build
# Binaries will be located in the bin/ directory
```

### 2. Install Models & Backends
Once the CLI is installed, use the `install` command to provision necessary dependencies (like `ffmpeg-serve`) for your platform:

```bash
# Automatically detects OS/Arch, downloads, and verifies checksums
./bin/ioimg install -m ffmpeg
```

---

## Usage & Discovery

### Feature Matrix
Check which commands are supported by your local hardware (CPU/GPU) vs Cloud providers (RunPod/Replicate):

```bash
./bin/ioimg list
./bin/iovid list
```

### Basic Example: Upscaling
```bash
./bin/ioimg upscale -i image.jpg -o output.jpg -p local_gpu
```

---

## Build Instructions

Requires Go 1.25.5+.

```bash
# Build native binaries for your current system
make build

# Run all tests
make test

# Generate universal release assets
make build-all
```

## License
MIT
