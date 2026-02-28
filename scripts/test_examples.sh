#!/bin/bash
# test_examples.sh - Reproducible CPU test suite for iosuite
# 
# This script verifies the core functionality of iosuite using standardized 
# example assets. It is designed to work out-of-the-box for both 
# developers and end-users.
#
# Usage: ./scripts/test_examples.sh [version]
#
# License: MIT (iosuite)

set -e

# --- Configuration ---
VERSION=${1:-"v0.1.0"}
REPO_URL="https://github.com/ls-ads/iosuite"
EXAMPLE_ARCHIVE="io-examples.zip"
BINARY_NAME="ioimg"

echo "iosuite | Starting Example Test Suite ($VERSION)"
echo "----------------------------------------------------"

# --- 1. Dependency Checks ---
echo "Checking dependencies..."
DEPENDENCIES=("unzip" "curl")
for dep in "${DEPENDENCIES[@]}"; do
    if ! command -v "$dep" &> /dev/null; then
        echo "Error: $dep is not installed. Please install it to continue."
        exit 1
    fi
done

# --- 2. Binary Resolution ---
echo "Locating binaries..."

resolve_binary() {
    local name=$1
    local bin=""
    if [[ -f "./bin/$name" ]]; then
        bin="./bin/$name"
    else
        local target=""
        if [[ "$OSTYPE" == "linux-gnu"* ]]; then
            target="./bin/${name}-linux-amd64"
        elif [[ "$OSTYPE" == "darwin"* ]]; then
            target="./bin/${name}-darwin-arm64"
        elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
            target="./bin/${name}-windows-amd64.exe"
        fi
        if [[ -f "$target" ]]; then
            bin="$target"
        elif command -v "$name" &> /dev/null; then
            bin=$(command -v "$name")
        fi
    fi
    echo "$bin"
}

BINARY_IOIMG=$(resolve_binary "ioimg")
BINARY_IOVID=$(resolve_binary "iovid")

if [[ -z "$BINARY_IOIMG" ]] || [[ -z "$BINARY_IOVID" ]]; then
    echo "Error: ioimg or iovid binary not found."
    echo "   Run 'make build' or download the release from $REPO_URL/releases"
    exit 1
fi
echo "Using ioimg: $BINARY_IOIMG"
echo "Using iovid: $BINARY_IOVID"

# --- 3. Asset Provisioning ---
if [[ ! -f "examples/portrait.png" ]]; then
    echo "Example assets missing. Downloading from release $VERSION..."
    
    # Attempt download via gh if available, else curl
    if command -v gh &> /dev/null; then
        gh release download "$VERSION" -p "$EXAMPLE_ARCHIVE" --clobber
    else
        DOWNLOAD_URL="$REPO_URL/releases/download/$VERSION/$EXAMPLE_ARCHIVE"
        curl -L -O "$DOWNLOAD_URL"
    fi
    
    if [[ -f "$EXAMPLE_ARCHIVE" ]]; then
        unzip -o "$EXAMPLE_ARCHIVE"
        rm "$EXAMPLE_ARCHIVE"
        echo "Assets provisioned successfully."
    else
        echo "Error: Could not download assets from $VERSION."
        echo "   Please check your network or specify a valid version: ./scripts/test_examples.sh v0.x.x"
        exit 1
    fi
fi

# --- 4. Execution ---
echo "Preparing output directories..."
mkdir -p examples/output/upscale
mkdir -p examples/output/scale
mkdir -p examples/output/crop

# 1. Upscale Portrait (CPU)
echo "Testing: upscale (portrait)..."
$BINARY_IOIMG upscale -i examples/portrait.png -o examples/output/upscale/portrait_4x.png -p local_cpu -m ffmpeg --overwrite

# 2. Scale Landscape (CPU)
echo "Testing: scale (landscape)..."
$BINARY_IOIMG scale -i examples/landscape.png -o examples/output/scale/landscape_1080p.png --width 1920 --height 1080 -p local_cpu --overwrite

# 3. Crop Portrait (CPU)
echo "Testing: crop (portrait face)..."
$BINARY_IOIMG crop -i examples/portrait.png -o examples/output/crop/portrait_face.png -w 400 -h 400 -x 300 -y 150 -p local_cpu --overwrite

# 4. Chunk & Concat Video
echo "Testing: chunk & concat (video.mp4)..."
mkdir -p examples/output/chunk
# Chunk into 3 equal parts
$BINARY_IOVID chunk -i examples/video.mp4 -o "examples/output/chunk/part_%03d.mp4" --chunks 3
# Concat them back
$BINARY_IOVID concat examples/output/chunk/part_000.mp4 examples/output/chunk/part_001.mp4 examples/output/chunk/part_002.mp4 -o examples/output/chunk/video_concat.mp4

# Verify concat success via ffprobe duration
echo "Verifying concatted video validity..."
if [ ! -s examples/output/chunk/video_concat.mp4 ]; then
    echo "Error: Concat output video is empty or missing"
    exit 1
fi

DUR=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 examples/output/chunk/video_concat.mp4)
if [ -z "$DUR" ]; then
    echo "Error: Concat output video is invalid (ffprobe failed)"
    exit 1
else
    echo "Success: Concat video is valid! (Duration: $DUR)"
fi

echo "----------------------------------------------------"
echo "All tests completed! Results are in: examples/output/"
ls -R examples/output/
