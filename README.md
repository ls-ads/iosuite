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

## Getting Started

### 1. Installation
Provision required binaries (like `ffmpeg-serve`) for your specific platform locally:

```bash
./bin/ioimg install -m ffmpeg
```

### 2. Feature Discovery
Check command-provider compatibility across local CPU, GPU, and cloud providers:

```bash
./ioimg list
./iovid list
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
