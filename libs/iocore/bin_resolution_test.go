package iocore

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestResolveBinary(t *testing.T) {
	// Create a dummy bin dir in a temp folder for testing
	tmpDir, err := os.MkdirTemp("", "iosuite-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", tmpDir)
	}
	defer os.RemoveAll(tmpDir)

	// Mock ~/.iosuite/bin path logic by temporarily overriding Home dir would be complex,
	// but we can at least test the system path lookups or just basic logic.

	// For now, let's test if we can resolve "ls" or "dir"
	target := "ls"
	if runtime.GOOS == "windows" {
		target = "cmd.exe"
	}

	path, err := ResolveBinary(target)
	if err != nil {
		t.Errorf("expected to find %s in PATH, got error: %v", target, err)
	}
	if path == "" {
		t.Errorf("expected non-empty path for %s", target)
	}
}

func TestResolveBinaryFFmpeg(t *testing.T) {
	// ffmpeg-serve prioritization test
	// This is hard to test without mocking the home dir, but we can check if it returns
	// the expected error message when not found.

	path, err := ResolveBinary("ffmpeg")
	if err == nil {
		t.Errorf("expected error when resolving 'ffmpeg', got path: %s", path)
	} else {
		expectedHint := "direct usage of 'ffmpeg' is banned"
		if !contains(err.Error(), expectedHint) {
			t.Errorf("expected error to contain %q, got: %v", expectedHint, err)
		}
	}

	pathServe, errServe := ResolveBinary("ffmpeg-serve")
	if errServe == nil {
		t.Logf("ffmpeg-serve found at: %s", pathServe)
	} else {
		expectedHint := "ioimg install -m ffmpeg"
		if !contains(errServe.Error(), expectedHint) {
			t.Errorf("expected error to contain %q, got: %v", expectedHint, errServe)
		}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
