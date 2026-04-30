package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Provider != "local" {
		t.Errorf("default provider = %q, want %q", cfg.Provider, "local")
	}
	if cfg.Model != "realesrgan-x4plus" {
		t.Errorf("default model = %q, want %q", cfg.Model, "realesrgan-x4plus")
	}
}

func TestLoad_MissingFileFallsBackToDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "missing"))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no file should not error, got: %v", err)
	}
	def := Defaults()
	if cfg != def {
		t.Errorf("Load with no file should return Defaults() exactly: got %+v want %+v", cfg, def)
	}
}

func TestLoad_PartialFileMergesOverDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "iosuite")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `# user-edited config
[default]
provider = "runpod"
# model intentionally unset — should fall through to default

[runpod]
endpoint_id = "abc123"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "runpod" {
		t.Errorf("Provider = %q, want %q (file should override default)", cfg.Provider, "runpod")
	}
	if cfg.Model != "realesrgan-x4plus" {
		t.Errorf("Model = %q, want %q (unset key should fall through to default)", cfg.Model, "realesrgan-x4plus")
	}
	if cfg.RunpodEndpointID != "abc123" {
		t.Errorf("RunpodEndpointID = %q, want %q", cfg.RunpodEndpointID, "abc123")
	}
}

func TestLoad_StripsQuotesAndInlineComments(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "iosuite")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[default]
provider   = "runpod"    # inline comment after value
output_dir = '/tmp/out'  # single-quote form
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "runpod" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "runpod")
	}
	if cfg.OutputDir != "/tmp/out" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "/tmp/out")
	}
}
