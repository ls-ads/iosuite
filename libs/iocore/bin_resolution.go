package iocore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ResolveBinary looks for a binary in the system PATH, and if not found,
// checks the ~/.iosuite/bin directory.
func ResolveBinary(name string) (string, error) {
	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".iosuite", "bin")

	// 1. Explicitly ban "ffmpeg" to avoid accidental system ffmpeg usage
	if name == "ffmpeg" {
		return "", fmt.Errorf("direct usage of 'ffmpeg' is banned. Please explicitly use 'ffmpeg-serve'")
	}

	if name == "ffmpeg-serve" {
		target := "ffmpeg-serve"
		if os.PathSeparator == '\\' {
			target += ".exe"
		}
		localPath := filepath.Join(binDir, target)
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}

		// If not in .iosuite/bin, try system path as fallback BUT suggest install
		path, err := exec.LookPath(target)
		if err == nil {
			return path, nil
		}

		return "", fmt.Errorf("ffmpeg-serve not found. Please run: ioimg install -m ffmpeg")
	}

	// 2. Try ~/.iosuite/bin for other tools (like realesrgan-ncnn-vulkan)
	target := name
	if os.PathSeparator == '\\' && !filepath.HasPrefix(target, ".\\") {
		if filepath.Ext(target) == "" {
			target += ".exe"
		}
	}
	localPath := filepath.Join(binDir, target)
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// 3. Try system path
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}

	installHint := name
	if name == "realesrgan-ncnn-vulkan" {
		installHint = "real-esrgan"
	}

	return "", fmt.Errorf("binary '%s' not found. Please run 'ioimg install -m %s' or install it manually", name, installHint)
}
