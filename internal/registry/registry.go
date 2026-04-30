// Package registry maps user-facing tool names to the *-serve repos
// that publish their deploy manifests.
//
// Adding a new tool: append one entry below. iosuite knows nothing
// else about the tool — image, disk, GPU pools, defaults all live in
// the tool's own deploy/runpod.json (see real-esrgan-serve's
// deploy/SCHEMA.md). This file is the only place iosuite holds
// implementation knowledge for any tool, and it's just a name → URL
// shortcut.
//
// `iosuite endpoint deploy --tool real-esrgan` resolves to:
//
//	https://raw.githubusercontent.com/ls-ads/real-esrgan-serve/<version>/deploy/runpod.json
//
// where <version> defaults to the StableVersion declared here, or
// can be overridden with --version.
package registry

import (
	"fmt"
	"sort"
	"strings"
)

// Entry points iosuite at a *-serve repo. The git tag pinned in
// StableVersion is the version this iosuite release was tested
// against; users wanting a newer / older manifest pass --version.
type Entry struct {
	Owner         string
	Repo          string
	StableVersion string
	// Description shows up in `iosuite endpoint deploy --help` so
	// users browsing tools see what each one does.
	Description string
}

// Tools is the canonical registry. Keep entries sorted by tool name
// alphabetically — makes the help output stable + reviewable.
var Tools = map[string]Entry{
	"real-esrgan": {
		Owner: "ls-ads",
		Repo:  "real-esrgan-serve",
		// Tracking main until the next image tag is cut. The
		// runpod-trt-0.2.1 tag predates the manifest split — fetching
		// the manifest at that tag returns 404. Once a release
		// after 2026-04-30 lands with deploy/runpod.json present,
		// pin this to that tag for reproducibility.
		StableVersion: "main",
		Description:   "Real-ESRGAN 4× image upscaler (TensorRT-accelerated).",
	},
	// Future:
	//   "whisper":          {Owner: "ls-ads", Repo: "whisper-serve",          StableVersion: "..."},
	//   "stable-diffusion": {Owner: "ls-ads", Repo: "stable-diffusion-serve", StableVersion: "..."},
}

// ManifestURL returns the raw-GitHub URL for the given tool +
// version's deploy/runpod.json. version may be empty to use the
// tool's StableVersion.
func ManifestURL(tool, version string) (string, error) {
	e, ok := Tools[tool]
	if !ok {
		return "", fmt.Errorf("unknown tool %q. Known: %s", tool, strings.Join(Names(), ", "))
	}
	v := version
	if v == "" {
		v = e.StableVersion
	}
	if v == "" {
		return "", fmt.Errorf("tool %q has no StableVersion and no --version was passed", tool)
	}
	return fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/%s/deploy/runpod.json",
		e.Owner, e.Repo, v,
	), nil
}

// Names returns the registered tools sorted alphabetically. Used in
// error messages so the user sees the closed set when they pass an
// unknown name.
func Names() []string {
	out := make([]string, 0, len(Tools))
	for k := range Tools {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
