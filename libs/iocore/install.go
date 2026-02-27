package iocore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

var ffmpegServeChecksums = map[string]string{
	"darwin-amd64":  "4d258ae65d42b39a7b354aacc81fe9c17c12b7f905003266e7a7e8ab6a0e43e5",
	"darwin-arm64":  "1a57dcafc2641344b7002ed2787410f9dba43551151de102fa58a73eb01d5f69",
	"linux-amd64":   "8326a66984420895e5dfba07bd73eeb8752c15a8d3f7627f312761e7cec8a7e0",
	"linux-arm64":   "8abd560c6db4d1cc5d62067b9b862af9121fdf3a4e9d57fbc0bd0cdd7834f42b",
	"windows-amd64": "15df0746fb9c668790a6fdd2840d3145bd4576424220ce74a4c475546b55b0ad",
	"windows-arm64": "8ff36441b6fa8b0425c5bcc9aaceb180b6f04c304000b73ac7a85710c9e2f9c1",
}

// InstallModel downloads and installs a supported model/binary for the current platform.
func InstallModel(ctx context.Context, model string) error {
	switch model {
	case "ffmpeg":
		// Proceed with ffmpeg installation logic below
	case "real-esrgan":
		Info("Initializing real-esrgan setup...", "status", "coming soon")
		fmt.Println("real-esrgan model setup is being initialized. Full automation will be available in the next release.")
		return nil
	default:
		return fmt.Errorf("model %s is not supported for installation yet", model)
	}

	osName := runtime.GOOS
	archName := runtime.GOARCH

	platform := fmt.Sprintf("%s-%s", osName, archName)
	checksum, ok := ffmpegServeChecksums[platform]
	if !ok {
		return fmt.Errorf("unsupported platform for binary download: %s", platform)
	}

	fileName := fmt.Sprintf("ffmpeg-serve-%s", platform)
	if osName == "windows" {
		fileName += ".exe"
	}

	url := fmt.Sprintf("https://github.com/ls-ads/ffmpeg-serve/releases/download/v0.1.0/%s", fileName)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %v", err)
	}

	binDir := filepath.Join(home, ".iosuite", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %v", err)
	}

	targetName := "ffmpeg-serve"
	if osName == "windows" {
		targetName += ".exe"
	}
	targetPath := filepath.Join(binDir, targetName)

	Info("Downloading binary", "url", url)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "ffmpeg-serve-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to save download: %v", err)
	}
	tmpFile.Close()

	// Verify checksum
	Info("Verifying checksum", "expected", checksum)
	if err := verifyChecksum(tmpFile.Name(), checksum); err != nil {
		return fmt.Errorf("checksum verification failed: %v", err)
	}

	// Move to target
	if err := os.Rename(tmpFile.Name(), targetPath); err != nil {
		// If rename fails (e.g. cross-device), copy instead
		if err := copyInstallFile(tmpFile.Name(), targetPath); err != nil {
			return fmt.Errorf("failed to install binary: %v", err)
		}
	}

	if err := os.Chmod(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to set executable permissions: %v", err)
	}

	Info("Successfully installed binary", "path", targetPath)
	return nil
}

func verifyChecksum(filePath, expectedChecksum string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(h.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}
	return nil
}

func copyInstallFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
