// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageJSONName returns the trimmed name from a package.json body.
// Mirrors `node -p "require('./package.json').name" | sed 's/@.*\///'`
// — strips any leading scope (`@scope/`).
func PackageJSONName(body []byte) string {
	type pkg struct {
		Name string `json:"name"`
	}

	var p pkg //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}

	if i := strings.Index(p.Name, "/"); i >= 0 && strings.HasPrefix(p.Name, "@") {
		return p.Name[i+1:]
	}

	return p.Name
}

// PackageJSONVersion returns the version field of a package.json body.
func PackageJSONVersion(body []byte) string {
	type pkg struct {
		Version string `json:"version"`
	}

	var p pkg
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}

	return p.Version
}

var (
	gradleVersionLine      = regexp.MustCompile(`(?m)version\s*=\s*['"]?([^'"\s]+)`)
	gradleRootProjectLine  = regexp.MustCompile(`(?m)rootProject\.name\s*=\s*['"]?([^'"\s]+)`)
	goModModule            = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	goModMajorVersionPath  = regexp.MustCompile(`(?m)^module\s+\S+/v(\d+)`)
	cargoNameLine          = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	cargoVersionLineDomain = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)
	pythonNameLine         = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	pythonVersionLine      = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)
	pythonSetupName        = regexp.MustCompile(`name\s*=\s*['"]([^'"]+)['"]`)
	pythonSetupVersion     = regexp.MustCompile(`version\s*=\s*['"]([^'"]+)['"]`)
)

// GradleVersion extracts the first `version = '…'` value from a
// build.gradle body. Mirrors the grep used in the bash.
func GradleVersion(body []byte) string {
	m := gradleVersionLine.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// GradleRootProjectName extracts rootProject.name from a settings.gradle
// body.
func GradleRootProjectName(body []byte) string {
	m := gradleRootProjectLine.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// GoModuleName returns the basename of the `module` declaration in a
// go.mod body. Mirrors `grep '^module' | xargs basename`.
func GoModuleName(body []byte) string {
	m := goModModule.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return filepath.Base(string(m[1]))
}

// GoModuleMajorVersion returns the synthetic major-version string
// (e.g. "2.0.0" when the module path ends in `/v2`). Mirrors the
// `s/v\K[0-9]+/$&.0.0/` bash logic.
func GoModuleMajorVersion(body []byte) string {
	m := goModMajorVersionPath.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1]) + ".0.0"
}

// CargoTOMLName returns the first `^name = "..."` in a Cargo.toml body.
func CargoTOMLName(body []byte) string {
	m := cargoNameLine.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// CargoTOMLVersion returns the first `^version = "..."` in a Cargo.toml
// body.
func CargoTOMLVersion(body []byte) string {
	m := cargoVersionLineDomain.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// PyProjectName returns `^name = "..."` from a pyproject.toml body.
func PyProjectName(body []byte) string {
	m := pythonNameLine.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// PyProjectVersion returns `^version = "..."` from a pyproject.toml
// body.
func PyProjectVersion(body []byte) string {
	m := pythonVersionLine.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// SetupPyName / SetupPyVersion read the equivalent fields from a
// classic setup.py body (loose regex, matches grep -oP).
func SetupPyName(body []byte) string {
	m := pythonSetupName.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}

// SetupPyVersion extracts version="..." from a setup.py body, returning
// "" when no match is found.
func SetupPyVersion(body []byte) string {
	m := pythonSetupVersion.FindSubmatch(body)
	if m == nil {
		return ""
	}

	return string(m[1])
}
