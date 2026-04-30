// Package runtime locates the `real-esrgan-serve` binary that iosuite
// subprocesses for actual GPU work.
//
// Lookup order:
//  1. --runtime flag (caller-provided override)
//  2. $REAL_ESRGAN_SERVE_BIN env var
//  3. real-esrgan-serve on $PATH (the typical install)
//  4. <iosuite-binary-dir>/real-esrgan-serve (release tarball that
//     ships both binaries side-by-side; not yet shipping but reserve
//     the slot so the install script has somewhere to drop the
//     companion binary)
//
// Errors out with a helpful message — `iosuite doctor` reads the same
// helper to diagnose missing-binary cases.
package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const BinaryName = "real-esrgan-serve"

// LocateRealEsrganServe returns the absolute path of the
// real-esrgan-serve binary that should be invoked, or an error with a
// human-readable explanation of where it tried to look.
func LocateRealEsrganServe(override string) (string, error) {
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	}
	if env := os.Getenv("REAL_ESRGAN_SERVE_BIN"); env != "" {
		candidates = append(candidates, env)
	}

	if path, err := exec.LookPath(BinaryName); err == nil {
		candidates = append(candidates, path)
	}

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), BinaryName))
	}

	for _, c := range candidates {
		if c == "" {
			continue
		}
		// LookPath handles both absolute paths (file existence + exec
		// bit) and bare names (PATH search). Either way, error here
		// means "this candidate isn't usable, try the next."
		if abs, err := exec.LookPath(c); err == nil {
			return abs, nil
		}
	}

	return "", fmt.Errorf(
		"%s not found. Looked at: %v.\n"+
			"Install: https://github.com/ls-ads/real-esrgan-serve/releases\n"+
			"Or set $REAL_ESRGAN_SERVE_BIN to the absolute path.",
		BinaryName, candidates,
	)
}

// Probe runs `real-esrgan-serve --version` to confirm the binary is
// actually executable + roughly the version we expect. Returns the
// version string on success.
func Probe(binPath string) (string, error) {
	cmd := exec.Command(binPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%s --version exited %d: %s",
				binPath, ee.ExitCode(), string(ee.Stderr))
		}
		return "", fmt.Errorf("run %s --version: %w", binPath, err)
	}
	return string(out), nil
}
