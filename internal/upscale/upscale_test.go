package upscale

import (
	"path/filepath"
	"testing"
)

func TestDerivedOutputPath_AlongsideInput(t *testing.T) {
	got := derivedOutputPath("/home/me/cat.jpg", "")
	want := filepath.Join("/home/me", "cat_4x.jpg")
	if got != want {
		t.Errorf("derivedOutputPath(cat.jpg, \"\") = %q, want %q", got, want)
	}
}

func TestDerivedOutputPath_WithOutDir(t *testing.T) {
	got := derivedOutputPath("/home/me/cat.jpg", "/tmp/out")
	want := filepath.Join("/tmp/out", "cat_4x.jpg")
	if got != want {
		t.Errorf("derivedOutputPath with outDir = %q, want %q", got, want)
	}
}

func TestDerivedOutputPath_NoExtensionDefaultsToPng(t *testing.T) {
	got := derivedOutputPath("/home/me/photo", "")
	want := filepath.Join("/home/me", "photo_4x.png")
	if got != want {
		t.Errorf("derivedOutputPath(photo, \"\") = %q, want %q", got, want)
	}
}

func TestDerivedOutputPath_PreservesExtensionCase(t *testing.T) {
	// PIL doesn't care about case, but preserving it keeps the
	// user's filename style intact (Photo.JPEG → Photo_4x.JPEG).
	got := derivedOutputPath("Photo.JPEG", "")
	want := "Photo_4x.JPEG"
	if got != want {
		t.Errorf("derivedOutputPath preserved case = %q, want %q", got, want)
	}
}
