# iosuite

A high-performance, unified suite for image and video processing. Leveraging FFmpeg, local GPU acceleration (NCNN/Vulkan/CUDA), and serverless cloud providers (RunPod/Replicate).

`iosuite` is designed to be truly universal, providing bit-for-bit identical results across **Linux**, **Windows**, and **macOS** by standardizing on optimized backends like `ffmpeg-serve` and `real-esrgan-serve`.

---

## 🚀 Quick Start (In 3 Commands)

1. **Build CLI**: Compile the native Go binaries for your system.
   ```bash
   make build
   ```

2. **Provision Backend**: Initialize your local or cloud processing environment.
   ```bash
   # Local: Install FFmpeg & Real-ESRGAN backends
   ./bin/ioimg install -m ffmpeg
   
   # Cloud: Automatic RunPod endpoint provisioning
   ./bin/ioimg init -m ffmpeg -p runpod -k YOUR_API_KEY
   ```

3. **Process Media**: Start processing with hardware acceleration.
   ```bash
   # Upscale an image using local GPU
   ./bin/ioimg upscale -i image.jpg -o output.jpg -p local_gpu
   ```

---

## 🏗️ Architecture

`iosuite` is built on a modular, Go-native core designed for maximum portability and performance.

### Components
- **`iocore`**: The engine. A pure-Go library providing the pipeline logic, hardware acceleration wrappers, and cloud bridge.
- **`ioimg`**: CLI tool for high-performance image processing and upscaling.
- **`iovid`**: CLI tool for comprehensive video transformations and filters.
- **`libiocore`**: A C-shared library bridge allowing the core logic to be imported by Python, Rust, or C++ applications.

### Standardized Backends
To ensure consistency across platforms, `iosuite` utilizes specialized wrappers:
- **`ffmpeg-serve`**: The primary local FFmpeg engine, providing a standardized CLI and REST API.
- **`real-esrgan-serve`**: A TensorRT-optimized bridge for ultra-fast AI upscaling on NVIDIA hardware.

---

## 🌍 Universal Compatibility

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

## 🧪 Verification & Testing

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

## 🛠️ Discovery & Help

Use the `list` command to explore supported features and provider compatibility for your specific hardware:

```bash
./bin/ioimg list
./bin/iovid list
```

## License
MIT
