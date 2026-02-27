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
echo "Locating $BINARY_NAME binary..."
BINARY=""

# Check PATH first
if command -v "$BINARY_NAME" &> /dev/null; then
    BINARY=$(command -v "$BINARY_NAME")
# Check local bin directory
elif [[ -f "./bin/$BINARY_NAME" ]]; then
    BINARY="./bin/$BINARY_NAME"
# Check for cross-compiled names
else
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        TARGET="./bin/ioimg-linux-amd64"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        TARGET="./bin/ioimg-darwin-arm64"
    elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
        TARGET="./bin/ioimg-windows-amd64.exe"
    fi
    
    if [[ -f "$TARGET" ]]; then
        BINARY="$TARGET"
    fi
fi

if [[ -z "$BINARY" ]]; then
    echo "Error: $BINARY_NAME binary not found."
    echo "   Run 'make build' or download the release from $REPO_URL/releases"
    exit 1
fi
echo "Using binary: $BINARY"

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
$BINARY upscale -i examples/portrait.png -o examples/output/upscale/portrait_4x.png -p local_cpu -m ffmpeg --overwrite

# 2. Scale Landscape (CPU)
echo "Testing: scale (landscape)..."
$BINARY scale -i examples/landscape.png -o examples/output/scale/landscape_1080p.png --width 1920 --height 1080 -p local_cpu --overwrite

# 3. Crop Portrait (CPU)
echo "Testing: crop (portrait face)..."
$BINARY crop -i examples/portrait.png -o examples/output/crop/portrait_face.png -w 400 -h 400 -x 300 -y 150 -p local_cpu --overwrite

echo "----------------------------------------------------"
echo "All tests completed! Results are in: examples/output/"
ls -R examples/output/
