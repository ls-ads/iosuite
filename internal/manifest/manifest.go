// Package manifest fetches + validates deploy manifests published by
// each `*-serve` module.
//
// The split: iosuite owns the deploy interface (saveTemplate +
// saveEndpoint plumbing, listing, teardown, benchmark). Each
// *-serve repo owns the implementation specifics (image tag, disk,
// GPU pools, CUDA pin, FlashBoot default). iosuite reads those from
// a JSON manifest at a known path in the serve repo, versioned by
// git tag — see real-esrgan-serve's deploy/SCHEMA.md for the
// authoritative field reference.
//
// Wire convention:
//
//	https://raw.githubusercontent.com/<owner>/<repo>/<tag>/deploy/runpod.json
//
// `Fetch` does the GET + JSON decode + schema validation. `LoadFile`
// reads the same shape from a local file (for the --manifest dev
// override).
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SchemaVersion is the version this code knows how to parse. Bump
// in lockstep with breaking schema changes in *-serve manifests;
// existing manifests at the old version still work because the
// fetcher rejects mismatches with a clear error.
const SchemaVersion = "1"

// Manifest is the parsed deploy/runpod.json. Field shape mirrors
// real-esrgan-serve's deploy/SCHEMA.md.
type Manifest struct {
	SchemaVersion string             `json:"schema_version"`
	Tool          string             `json:"tool"`
	Description   string             `json:"description,omitempty"`
	Image         string             `json:"image"`
	Endpoint      EndpointDefaults   `json:"endpoint"`
	GPUPools      map[string]string  `json:"gpu_pools"`
	Env           []EnvVar           `json:"env"`
}

// EndpointDefaults groups the per-tool defaults the deploy command
// applies before any user overrides.
type EndpointDefaults struct {
	ContainerDiskGB     int    `json:"container_disk_gb"`
	WorkersMaxDefault   int    `json:"workers_max_default"`
	IdleTimeoutSDefault int    `json:"idle_timeout_s_default"`
	FlashbootDefault    bool   `json:"flashboot_default"`
	// MinCudaVersion is "" for tools that don't pin a driver. Empty
	// string and absent field both decode to "" — the deploy code
	// treats both as "no pin".
	MinCudaVersion string `json:"min_cuda_version"`
}

// EnvVar is one container environment variable applied to the
// worker template at deploy time.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// HTTPClient is the http.Client Fetch uses. Tests swap this to a
// client whose transport trusts httptest's self-signed cert; prod
// callers leave the default. Package-level for the same reason
// http.DefaultClient is — single config that's easy to override
// without threading a client through every callsite.
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

// Fetch downloads a manifest from the given URL, decodes it, and
// validates it. Errors carry the URL + the failing field so the
// operator can see what's wrong without re-curling.
func Fetch(ctx context.Context, url string) (*Manifest, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("manifest URL must be HTTPS, got %q", url)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: build request: %w", url, err)
	}
	req.Header.Set("User-Agent", "iosuite-manifest-fetcher")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("manifest %s: read body: %w", url, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf(
			"manifest %s: HTTP 404 — check that the tool's git tag exists and that the repo publishes deploy/runpod.json at that ref",
			url,
		)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest %s: HTTP %d: %s", url, resp.StatusCode, truncate(string(body), 200))
	}

	return parseAndValidate(body, url)
}

// LoadFile reads a manifest from disk. Used by the --manifest dev
// flag so a maintainer can iterate on a manifest locally before
// pushing it to the *-serve repo.
func LoadFile(path string) (*Manifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	return parseAndValidate(body, path)
}

func parseAndValidate(body []byte, source string) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest %s: parse JSON: %w", source, err)
	}
	if err := m.validate(source); err != nil {
		return nil, err
	}
	return &m, nil
}

// validate returns the first schema violation found, with enough
// context that the operator can fix the manifest without re-reading
// SCHEMA.md.
func (m *Manifest) validate(source string) error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"manifest %s: schema_version is %q, this iosuite knows %q. Either upgrade iosuite or pin --version to a tag whose manifest matches",
			source, m.SchemaVersion, SchemaVersion,
		)
	}
	if m.Tool == "" {
		return fmt.Errorf("manifest %s: missing required field `tool`", source)
	}
	if m.Image == "" {
		return fmt.Errorf("manifest %s: missing required field `image`", source)
	}
	if !strings.Contains(m.Image, ":") {
		return fmt.Errorf("manifest %s: image %q has no tag (expected repo:tag)", source, m.Image)
	}
	if m.Endpoint.ContainerDiskGB <= 0 {
		return fmt.Errorf("manifest %s: endpoint.container_disk_gb must be > 0, got %d", source, m.Endpoint.ContainerDiskGB)
	}
	if m.Endpoint.WorkersMaxDefault <= 0 {
		return fmt.Errorf("manifest %s: endpoint.workers_max_default must be > 0, got %d", source, m.Endpoint.WorkersMaxDefault)
	}
	if m.Endpoint.IdleTimeoutSDefault <= 0 {
		return fmt.Errorf("manifest %s: endpoint.idle_timeout_s_default must be > 0, got %d", source, m.Endpoint.IdleTimeoutSDefault)
	}
	if len(m.GPUPools) == 0 {
		return fmt.Errorf("manifest %s: gpu_pools must declare at least one entry", source)
	}
	for cls, pool := range m.GPUPools {
		if cls == "" || pool == "" {
			return fmt.Errorf("manifest %s: gpu_pools entry has empty key or value", source)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
