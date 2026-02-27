#!/bin/bash

# ioSuite Uninstallation Script for Linux and macOS

INSTALL_DIR="$HOME/.local/bin"

echo "Uninstalling ioSuite tools from $INSTALL_DIR..."

rm -f "$INSTALL_DIR/ioimg"
rm -f "$INSTALL_DIR/iovid"

# Remove shell completions
remove_completion() {
    local rc_file=$1
    local marker="# ioSuite shell completion"

    if [[ -f "$rc_file" ]]; then
        if grep -q "$marker" "$rc_file"; then
            echo "Removing shell completion from $rc_file"
            # Use temp file to avoid issues with sed in-place across OSes
            grep -v "# ioSuite shell completion" "$rc_file" | grep -v "ioimg completion" | grep -v "iovid completion" > "${rc_file}.tmp"
            mv "${rc_file}.tmp" "$rc_file"
        fi
    fi
}

remove_completion "$HOME/.bashrc"
remove_completion "$HOME/.zshrc"
remove_completion "$HOME/.config/fish/config.fish"

echo "Uninstallation complete!"
