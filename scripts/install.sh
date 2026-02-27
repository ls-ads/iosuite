# ioSuite Installation Script for Linux and macOS
# Downloads pre-built binaries from GitHub Releases

set -e

RELEASE_VERSION="v0.1.0"
BASE_URL="https://github.com/ls-ads/iosuite/releases/download/$RELEASE_VERSION"
INSTALL_DIR="$HOME/.local/bin"

mkdir -p "$INSTALL_DIR"

# Detect OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Function to download binary
download_binary() {
    local name=$1
    local binary_name="${name}-${OS}-${ARCH}"
    local url="$BASE_URL/$binary_name"
    local dst="$INSTALL_DIR/$name"

    echo "Downloading $name ($OS/$ARCH) from $url..."
    
    if command -v curl > /dev/null; then
        curl -L "$url" -o "$dst"
    elif command -v wget > /dev/null; then
        wget -qO "$dst" "$url"
    else
        echo "Error: Neither curl nor wget found. Please install one to continue."
        exit 1
    fi

    chmod +x "$dst"
}

download_binary "ioimg"
download_binary "iovid"

# Shell Completion Setup
setup_completion() {
    local shell_name=$1
    local rc_file=$2
    local marker="# ioSuite shell completion"
    
    if [[ -f "$rc_file" ]]; then
        if ! grep -q "$marker" "$rc_file"; then
            echo "Adding shell completion to $rc_file"
            echo "" >> "$rc_file"
            echo "$marker" >> "$rc_file"
            echo "if command -v ioimg &> /dev/null; then source <(ioimg completion $shell_name); fi" >> "$rc_file"
            echo "if command -v iovid &> /dev/null; then source <(iovid completion $shell_name); fi" >> "$rc_file"
        fi
    fi
}

# Detect Shell and Setup Completions
CURRENT_SHELL=$(basename "$SHELL")

case "$CURRENT_SHELL" in
    bash)
        setup_completion "bash" "$HOME/.bashrc"
        ;;
    zsh)
        setup_completion "zsh" "$HOME/.zshrc"
        ;;
    fish)
        FISH_CONFIG="$HOME/.config/fish/config.fish"
        mkdir -p "$(dirname "$FISH_CONFIG")"
        setup_completion "fish" "$FISH_CONFIG"
        ;;
    *)
        echo "Follow-up: Manually add 'source <(ioimg completion <shell>)' to your shell config."
        ;;
esac

echo "Installation complete! Please restart your terminal or run: source ~/.${CURRENT_SHELL}rc"
