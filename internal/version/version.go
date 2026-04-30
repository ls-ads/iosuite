// Package version exposes the build-stamped semver + commit SHA so
// `iosuite version` and the `--version` flag agree.
//
// Both fields are overridden at build time via -ldflags "-X ..."
// (see Makefile). The "dev" / "unknown" defaults make local
// `go build` outputs obviously not-a-release.
package version

var (
	Version = "dev"
	Commit  = "unknown"
)
