// Package doctor implements `iosuite doctor` — diagnose what's
// missing on the host so the user knows what to install before their
// first upscale. Returns a non-zero exit when any required check
// fails; warnings (optional providers like RunPod creds) just print.
//
// Layout follows `kubectl version` / `gh auth status` — one line per
// check, with ✓ / ⚠ / ✗ and a one-line remedy when relevant.
package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"iosuite.io/internal/config"
	rrt "iosuite.io/internal/runtime"
)

// Run prints diagnostic results to w and returns true when every
// required check passed. Optional checks emit warnings but don't
// affect the return value.
func Run(ctx context.Context, w io.Writer, cfg config.Config) bool {
	allOK := true

	fmt.Fprintln(w, "iosuite doctor — diagnosing host")
	fmt.Fprintln(w, strings.Repeat("─", 56))

	// 1. Operating system + architecture (informational; no pass/fail)
	fmt.Fprintf(w, "  ℹ  platform        %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// 2. real-esrgan-serve binary on PATH
	bin, err := rrt.LocateRealEsrganServe("")
	if err != nil {
		allOK = false
		fmt.Fprintf(w, "  ✗  real-esrgan-serve  not found\n")
		fmt.Fprintf(w, "      → install: https://github.com/ls-ads/real-esrgan-serve/releases\n")
	} else {
		fmt.Fprintf(w, "  ✓  real-esrgan-serve  %s\n", bin)
		// 3. Probe it actually runs (catches "binary exists but
		// linker dies on missing libc" cases on weird hosts). The
		// 10s timeout is generous for a cold cache; a warm probe
		// returns in milliseconds.
		_, cancel := context.WithTimeout(ctx, 10*time.Second)
		ver, perr := rrt.Probe(bin)
		cancel()
		if perr != nil {
			allOK = false
			fmt.Fprintf(w, "  ✗  real-esrgan-serve --version failed: %v\n", perr)
		} else {
			ver = strings.TrimSpace(ver)
			fmt.Fprintf(w, "  ✓  real-esrgan-serve  %s\n", ver)
		}
	}

	// 4. python3 (the helper script needs it for ORT/TRT inference)
	if py, err := exec.LookPath("python3"); err != nil {
		allOK = false
		fmt.Fprintf(w, "  ✗  python3            not on PATH\n")
		fmt.Fprintf(w, "      → install python 3.10+ via your package manager\n")
	} else {
		fmt.Fprintf(w, "  ✓  python3            %s\n", py)
	}

	// 5. Optional: RunPod creds. Only relevant when the user wants
	// to use the runpod provider; not having them is a warning.
	if cfg.RunpodAPIKey == "" && os.Getenv("RUNPOD_API_KEY") == "" {
		fmt.Fprintf(w, "  ⚠  runpod credentials not configured (only matters for --provider runpod)\n")
	} else {
		fmt.Fprintf(w, "  ✓  runpod credentials configured\n")
	}

	// 6. Provider sanity
	switch cfg.Provider {
	case "local", "runpod", "serve":
		fmt.Fprintf(w, "  ℹ  default provider  %s\n", cfg.Provider)
	default:
		allOK = false
		fmt.Fprintf(w, "  ✗  default provider  %q (expected local | runpod | serve)\n", cfg.Provider)
	}

	fmt.Fprintln(w, strings.Repeat("─", 56))
	if allOK {
		fmt.Fprintln(w, "all required checks passed")
	} else {
		fmt.Fprintln(w, "one or more required checks failed; see ✗ rows above")
	}
	return allOK
}
