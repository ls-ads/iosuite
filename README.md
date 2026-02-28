# iosuite

A high-performance, unified suite for image and video processing. Leveraging FFmpeg and serverless cloud providers (RunPod/Replicate).

`iosuite` is designed to be truly universal, providing bit-for-bit identical results across **Linux**, **Windows**, and **macOS** by standardizing on backends like `ffmpeg-serve` and `real-esrgan-serve`.

---

## Quick Start (In 3 Commands)

1. **Install CLI**: Download the tools + autocompletions in one go.
   ```bash
   curl -sSL https://raw.githubusercontent.com/ls-ads/iosuite/main/scripts/install.sh | bash
   source ~/.$(basename $SHELL)rc
   ```

2. **Start Infrastructure**: Provision a specialized AI endpoint and Network Volume on RunPod.
   ```bash
   # Provision an endpoint + a 1GB Network Volume for fast processing of large files
   ioimg start --model real-esrgan --provider runpod --api-key YOUR_API_KEY --volume-size 1
   ```

3. **Upscale Media**: Process your images or videos with lightning-fast AI.
   ```bash
   # Upscale using your Network Volume (idempotent & efficient for large files)
   ioimg upscale --input photo.jpg --output photo_4x.jpg --model real-esrgan --provider runpod --volume YOUR_VOLUME_ID
   ```

For a full list of available commands and detailed usage for each tool, see:
- [ioimg Commands Reference](tools/ioimg/README.md)
- [iovid Commands Reference](tools/iovid/README.md)

## Installation & Shell Completion

Get `ioimg` and `iovid` in your system path with full autocompletion (Bash, Zsh, Fish, PowerShell).

### Linux & macOS
```bash
# Run the installation script (downloads v0.1.0 binaries)
curl -sSL https://raw.githubusercontent.com/ls-ads/iosuite/main/scripts/install.sh | bash

# Reload your shell
source ~/.$(basename $SHELL)rc
```

### Windows (PowerShell)
```powershell
# Run the installation script (downloads v0.1.0 binaries)
Invoke-RestMethod -Uri https://raw.githubusercontent.com/ls-ads/iosuite/main/scripts/install.ps1 | Invoke-Expression
```

> [!NOTE]
> **Autocompletion**: The install scripts automatically add `source <(ioimg completion <shell>)` to your shell profile. Once installed, try typing `ioimg [TAB]` to see it in action!

---

---

## Architecture

`iosuite` is built on a modular, Go-native core designed for maximum portability and performance.

### Components
- **`iocore`**: The engine. A pure-Go library providing the pipeline logic, hardware acceleration wrappers, and cloud bridge.
- **`ioimg`**: CLI tool for high-performance image processing and upscaling.
- **`iovid`**: CLI tool for comprehensive video transformations and filters.
- **`libiocore`**: A C-shared library bridge allowing the core logic to be imported by Python, Rust, or C++ applications.

### Standardized Backends

To ensure consistency across platforms, `iosuite` utilizes specialized wrappers:
- **[ffmpeg-serve](https://github.com/ls-ads/ffmpeg-serve)**: The primary local FFmpeg engine, providing a standardized CLI and REST API.
- **[real-esrgan-serve](https://github.com/ls-ads/real-esrgan-serve)**: A TensorRT-optimized bridge for ultra-fast AI upscaling on NVIDIA hardware.

---

## Universal Compatibility

| OS | Architectures | Accelerated By |
| :--- | :--- | :--- |
| **Linux** | x86_64, ARM64 | NVIDIA CUDA / NVENC |
| **Windows** | x86_64, ARM64 | NVIDIA CUDA / NVENC |
| **macOS** | Apple Silicon, Intel | VideoToolbox (Hardware) |

### Zero-Config Cross-Compilation
The CLI tools are written in **pure Go** and avoid CGO dependencies.
```bash
# Generate binaries for all platforms
make build-all
```

---

## Verification & Testing

We maintain a dual-layered testing strategy to ensure reliability:

### 1. Unit Tests
Fast verification of core library logic (validation, resolution, mapping).
```bash
go test ./libs/iocore
```

### 2. Integration Tests
End-to-end "Smoke Tests" using standardized example assets.
```bash
./scripts/test_examples.sh
```

> [!TIP]
> **Unified Testing**: Run `make test` to execute both suites in a single pass. If the example assets are missing, the script will automatically pull them from the latest GitHub release.

---

## Discovery & Help

Use the `list` command to explore supported features and provider compatibility for your specific hardware:

```bash
./bin/ioimg list
./bin/iovid list
```

## License
MIT
