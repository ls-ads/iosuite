package registry

import (
	"strings"
	"testing"
)

func TestManifestURL_DefaultsToStable(t *testing.T) {
	url, err := ManifestURL("real-esrgan", "")
	if err != nil {
		t.Fatalf("ManifestURL: %v", err)
	}
	const wantPrefix = "https://raw.githubusercontent.com/ls-ads/real-esrgan-serve/"
	if !strings.HasPrefix(url, wantPrefix) {
		t.Errorf("URL prefix = %q, want %q", url, wantPrefix)
	}
	if !strings.HasSuffix(url, "/deploy/runpod.json") {
		t.Errorf("URL suffix = %q, want trailing /deploy/runpod.json", url)
	}
	if !strings.Contains(url, Tools["real-esrgan"].StableVersion) {
		t.Errorf("URL should contain the stable version %q: %v", Tools["real-esrgan"].StableVersion, url)
	}
}

func TestManifestURL_RespectsVersion(t *testing.T) {
	url, err := ManifestURL("real-esrgan", "v9.9.9-test")
	if err != nil {
		t.Fatalf("ManifestURL: %v", err)
	}
	if !strings.Contains(url, "v9.9.9-test") {
		t.Errorf("URL should contain the requested version: %v", url)
	}
}

func TestManifestURL_RejectsUnknownTool(t *testing.T) {
	_, err := ManifestURL("imagined-tool", "")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "imagined-tool") {
		t.Errorf("error should name the tool: %v", err)
	}
	// The error should also list available tools so the user can
	// recover without re-reading the help text.
	if !strings.Contains(err.Error(), "real-esrgan") {
		t.Errorf("error should include the closed-set list: %v", err)
	}
}

func TestNames_Sorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("Names() is not sorted: %v", names)
			break
		}
	}
}
