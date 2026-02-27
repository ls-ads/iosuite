package iocore

import (
	"path/filepath"
	"strings"
)

// IsImage checks if the given path is a supported image format or pattern.
func IsImage(path string) bool {
	if strings.Contains(path, "%") {
		lower := strings.ToLower(path)
		return strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// IsVideo checks if the given path is a supported video format.
func IsVideo(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".flv":
		return true
	}
	return false
}

// IsAudio checks if the given path is a supported audio format.
func IsAudio(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3", ".wav", ".m4a", ".aac", ".flac":
		return true
	}
	return false
}
