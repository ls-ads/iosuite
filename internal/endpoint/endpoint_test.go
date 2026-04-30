package endpoint

import (
	"context"
	"strings"
	"testing"

	"iosuite.io/internal/manifest"
)

func validManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: "1",
		Tool:          "real-esrgan",
		Image:         "ghcr.io/ls-ads/real-esrgan-serve:test",
		Endpoint: manifest.EndpointDefaults{
			ContainerDiskGB:     10,
			WorkersMaxDefault:   2,
			IdleTimeoutSDefault: 30,
			FlashbootDefault:    true,
			MinCudaVersion:      "12.8",
		},
		GPUPools: map[string]string{"rtx-4090": "ADA_24"},
	}
}

func TestDeploy_RejectsUnsupportedProvider(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: "modal",
		APIKey:   "test",
		Tool:     "real-esrgan",
		GPUClass: "rtx-4090",
		Manifest: validManifest(),
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if !strings.Contains(err.Error(), "modal") {
		t.Errorf("error should name the offending provider: %v", err)
	}
}

func TestDeploy_RejectsMissingAPIKey(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: ProviderRunPod,
		Tool:     "real-esrgan",
		GPUClass: "rtx-4090",
		Manifest: validManifest(),
	})
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	for _, want := range []string{"runpod-api-key", "RUNPOD_API_KEY", "config"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestDeploy_RejectsMissingManifest(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: ProviderRunPod,
		APIKey:   "test",
		Tool:     "real-esrgan",
		GPUClass: "rtx-4090",
	})
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
	if !strings.Contains(err.Error(), "Manifest is required") {
		t.Errorf("error should mention required Manifest: %v", err)
	}
}

func TestDeploy_RejectsUnknownGPUClass(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: ProviderRunPod,
		APIKey:   "test",
		Tool:     "real-esrgan",
		GPUClass: "imagined-gpu",
		Manifest: validManifest(),
	})
	if err == nil {
		t.Fatal("expected error for unknown gpu-class, got nil")
	}
	if !strings.Contains(err.Error(), "imagined-gpu") {
		t.Errorf("error should name the unknown gpu-class: %v", err)
	}
	// The error should mention what GPU classes ARE in the manifest
	// so the user can recover without re-fetching it.
	if !strings.Contains(err.Error(), "rtx-4090") {
		t.Errorf("error should list valid GPU classes from the manifest: %v", err)
	}
}
