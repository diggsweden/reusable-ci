// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

// GoPlatforms is the canonical GOOS/GOARCH set the toolchain supports
// for Go 1.26. Sourced from `go tool dist list`; embedded so the CLI
// can validate `--platforms` input without shelling out to go (which
// would not work in lightweight runtime images that bake only the
// reusable-ci binary).
//
// Update when bumping the Go version pin in `.mise.toml`.
//
//nolint:gochecknoglobals // canonical enumeration — read-only.
var GoPlatforms = map[string]struct{}{
	"aix/ppc64":       {},
	"android/386":     {},
	"android/amd64":   {},
	"android/arm":     {},
	"android/arm64":   {},
	"darwin/amd64":    {},
	"darwin/arm64":    {},
	"dragonfly/amd64": {},
	"freebsd/386":     {},
	"freebsd/amd64":   {},
	"freebsd/arm":     {},
	"freebsd/arm64":   {},
	"illumos/amd64":   {},
	"ios/amd64":       {},
	"ios/arm64":       {},
	"js/wasm":         {},
	"linux/386":       {},
	"linux/amd64":     {},
	"linux/arm":       {},
	"linux/arm64":     {},
	"linux/loong64":   {},
	"linux/mips":      {},
	"linux/mips64":    {},
	"linux/mips64le":  {},
	"linux/mipsle":    {},
	"linux/ppc64":     {},
	"linux/ppc64le":   {},
	"linux/riscv64":   {},
	"linux/s390x":     {},
	"netbsd/386":      {},
	"netbsd/amd64":    {},
	"netbsd/arm":      {},
	"netbsd/arm64":    {},
	"openbsd/386":     {},
	"openbsd/amd64":   {},
	"openbsd/arm":     {},
	"openbsd/arm64":   {},
	"openbsd/ppc64":   {},
	"openbsd/riscv64": {},
	"plan9/386":       {},
	"plan9/amd64":     {},
	"plan9/arm":       {},
	"solaris/amd64":   {},
	"wasip1/wasm":     {},
	"windows/386":     {},
	"windows/amd64":   {},
	"windows/arm64":   {},
}

// IsKnownGoPlatform reports whether platform is one of the GOOS/GOARCH
// pairs supported by the pinned Go toolchain.
func IsKnownGoPlatform(platform string) bool {
	_, ok := GoPlatforms[platform]

	return ok
}
