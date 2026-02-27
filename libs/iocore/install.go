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
	"darwin-amd64":  "1b0395187311ca670dace6a9507d11e387a4fa15d2ea866825e7e69268638ff3",
	"darwin-arm64":  "9b202b93871b596fc62c74cd19b7054a6de77482d5cb3219f97964ae4d354b64",
	"linux-amd64":   "cfc6d2d437512cd77c48f71a52cd6e25c081888d8d15dd543575051f02e5b089",
	"linux-arm64":   "749309598882d0109efe417c30ac59c6fdd8fbc3d48ac2a0781514ded42b8693",
	"windows-amd64": "d88b163e4163291768dca3360d4e2097bb3bd9814a90bcb6cf6aaac3ae77f0ef",
	"windows-arm64": "7d1b1757993e9cb12da489eab03e3d819167ec1271afd0aed1d36e40ff0297f8",
}

// InstallModel downloads and installs a supported model/binary for the current platform.
func InstallModel(ctx context.Context, model string) error {
	if model != "ffmpeg" {
		return fmt.Errorf("model %s is not supported for installation yet (leaving real-esrgan alone per request)", model)
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
