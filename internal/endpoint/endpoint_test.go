package endpoint

import (
	"context"
	"strings"
	"testing"
)

func TestDeploy_RejectsUnsupportedProvider(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: "modal",
		APIKey:   "test",
		Tool:     "real-esrgan",
		GPUClass: "rtx-4090",
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
	})
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	// Error should mention all three sources so the user knows
	// where to set the key.
	for _, want := range []string{"runpod-api-key", "RUNPOD_API_KEY", "config"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestDeploy_RejectsUnknownTool(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: ProviderRunPod,
		APIKey:   "test",
		Tool:     "imagined-tool",
		GPUClass: "rtx-4090",
	})
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "imagined-tool") {
		t.Errorf("error should name the unknown tool: %v", err)
	}
}

func TestDeploy_RejectsUnknownGPUClass(t *testing.T) {
	_, err := Deploy(context.Background(), DeployInput{
		Provider: ProviderRunPod,
		APIKey:   "test",
		Tool:     "real-esrgan",
		GPUClass: "imagined-gpu",
	})
	if err == nil {
		t.Fatal("expected error for unknown gpu-class, got nil")
	}
	if !strings.Contains(err.Error(), "imagined-gpu") {
		t.Errorf("error should name the unknown gpu-class: %v", err)
	}
}

func TestTools_RealESRGAN_Pinned(t *testing.T) {
	tool, ok := Tools["real-esrgan"]
	if !ok {
		t.Fatal("real-esrgan tool entry missing")
	}
	if !strings.HasPrefix(tool.Image, "ghcr.io/ls-ads/real-esrgan-serve:") {
		t.Errorf("real-esrgan image should be pinned to ghcr.io/ls-ads/real-esrgan-serve, got %q", tool.Image)
	}
	if tool.ContainerDiskGB < 5 {
		t.Errorf("ContainerDiskGB looks too small (%d) — runpod-trt unpacks to ~3 GB", tool.ContainerDiskGB)
	}
}

func TestGPUPools_CommonGPUsPresent(t *testing.T) {
	// Spot-check a few well-known classes; full coverage doesn't
	// add signal — RunPod's pool roster changes faster than we'd
	// keep tests up to date.
	cases := map[string]string{
		"rtx-4090": "ADA_24",
		"rtx-3090": "AMPERE_24",
		"l40s":     "ADA_48_PRO",
		"a100":     "AMPERE_80",
		"h100":     "HOPPER_141",
	}
	for class, wantPool := range cases {
		if got, ok := GPUPools[class]; !ok {
			t.Errorf("GPUPools[%q] missing", class)
		} else if got != wantPool {
			t.Errorf("GPUPools[%q] = %q, want %q", class, got, wantPool)
		}
	}
}
