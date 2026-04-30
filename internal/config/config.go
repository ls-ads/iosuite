// Package config loads + saves the iosuite user config from
// `~/.config/iosuite/config.toml`. Precedence is enforced by callers:
// flag > env > config file > built-in defaults. We just provide the
// "config file" layer.
//
// Format is a hand-rolled tiny TOML — three sections, all string
// fields. Pulling in a full TOML library for this would be silly and
// add a build-graph dependency for ten lines of K=V parsing.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the on-disk shape. Empty strings mean "fall through to env
// / built-in default" — never panic on a missing field.
type Config struct {
	// [default]
	Provider  string // "local" | "runpod"
	OutputDir string // empty = alongside input
	Model     string // e.g. "realesrgan-x4plus"

	// [runpod]
	RunpodAPIKey     string
	RunpodEndpointID string
}

// Defaults are baked-in fallbacks. Used when the config file is
// missing OR a field is empty in the file.
func Defaults() Config {
	return Config{
		Provider: "local",
		Model:    "realesrgan-x4plus",
	}
}

// Path returns the canonical config file location. Honours
// $XDG_CONFIG_HOME, falls back to ~/.config/iosuite/config.toml.
func Path() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "iosuite", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".config", "iosuite", "config.toml"), nil
}

// Load reads the config file and merges it on top of Defaults().
// A missing file is not an error — first-time users just get defaults.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	if err := merge(&cfg, f); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// merge applies the contents of an open TOML stream onto cfg. Fields
// with empty values in the file are ignored (so partial config files
// fall through to defaults). The parser handles `# comment` lines,
// `[section]` headers, and `key = "value"` / `key = value` forms.
func merge(cfg *Config, r *os.File) error {
	scanner := bufio.NewScanner(r)
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip an inline `# comment` after the value so users can
		// annotate without breaking parsing.
		if h := strings.Index(val, " #"); h >= 0 {
			val = strings.TrimSpace(val[:h])
		}
		val = strings.Trim(val, `"'`)
		if val == "" {
			continue
		}
		apply(cfg, section, key, val)
	}
	return scanner.Err()
}

func apply(cfg *Config, section, key, val string) {
	switch section {
	case "default", "":
		switch key {
		case "provider":
			cfg.Provider = val
		case "output_dir":
			cfg.OutputDir = val
		case "model":
			cfg.Model = val
		}
	case "runpod":
		switch key {
		case "api_key":
			cfg.RunpodAPIKey = val
		case "endpoint_id":
			cfg.RunpodEndpointID = val
		}
	}
}
