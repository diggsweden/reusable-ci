// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

// CargoTargetTriples maps the GOOS/GOARCH spelling used by adopters
// (chosen for parity with Go's `--platforms`) to the canonical Rust
// target triple that `cargo build --target …` expects.
//
// The mapping is canonical: every entry here is a triple Rustup knows.
// Whether the *runtime image* can actually link a given target is a
// separate concern — the reusable-ci-runtime-rust image installs the
// targets and cross-linkers it supports, and `cargo build` will fail
// loudly when a linker is missing. Keeping the mapping broad lets us
// extend runtime-image support without touching this code.
//
//nolint:gochecknoglobals // canonical lookup table — read-only.
var CargoTargetTriples = map[string]string{
	"linux/amd64":   "x86_64-unknown-linux-gnu",
	"linux/arm64":   "aarch64-unknown-linux-gnu",
	"linux/arm":     "armv7-unknown-linux-gnueabihf",
	"darwin/amd64":  "x86_64-apple-darwin",
	"darwin/arm64":  "aarch64-apple-darwin",
	"windows/amd64": "x86_64-pc-windows-gnu",
}

// IsKnownCargoPlatform reports whether platform has a Rust target triple.
func IsKnownCargoPlatform(platform string) bool {
	_, ok := CargoTargetTriples[platform]

	return ok
}

// CargoTargetTriple returns the Rust target triple for platform, or "" if
// platform is not in the canonical mapping.
func CargoTargetTriple(platform string) string {
	return CargoTargetTriples[platform]
}
