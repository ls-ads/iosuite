package iocore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CleanupLocalFFmpeg removes the local ffmpeg-serve binary and related temporary files.
func CleanupLocalFFmpeg(ctx context.Context) error {
	// Stop any running ffmpeg-serve processes
	Info("Stopping any running ffmpeg-serve processes")
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/IM", "ffmpeg-serve.exe", "/T").Run()
	} else {
		_ = exec.Command("pkill", "ffmpeg-serve").Run()
	}

	// Clean up temporary files in system temp dir
	tmpDir := os.TempDir()
	entries, err := os.ReadDir(tmpDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "iocore-in-") || strings.HasPrefix(name, "iocore-out-") {
				path := filepath.Join(tmpDir, name)
				Info("Removing temporary file", "path", path)
				os.Remove(path)
			}
		}
	}

	return nil
}
