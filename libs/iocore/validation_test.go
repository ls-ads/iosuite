package iocore

import "testing"

func TestIsImage(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"test.jpg", true},
		{"test.JPG", true},
		{"test.png", true},
		{"test.gif", false},
		{"frame_%05d.png", true},
		{"no_extension", false},
	}

	for _, tt := range tests {
		if got := IsImage(tt.path); got != tt.want {
			t.Errorf("IsImage(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsVideo(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"test.mp4", true},
		{"test.mkv", true},
		{"test.mov", true},
		{"test.txt", false},
	}

	for _, tt := range tests {
		if got := IsVideo(tt.path); got != tt.want {
			t.Errorf("IsVideo(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIsAudio(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"test.mp3", true},
		{"test.wav", true},
		{"test.flac", true},
		{"test.jpg", false},
	}

	for _, tt := range tests {
		if got := IsAudio(tt.path); got != tt.want {
			t.Errorf("IsAudio(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
