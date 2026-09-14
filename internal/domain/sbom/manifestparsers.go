// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
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

// The Gradle version is the project's own assignment at the start of a line;
// a longer identifier such as ext.kotlin_version is not it, and a computed
// value such as project.findProperty(...) is not a version. The setup.py
// readers anchor on a word boundary so package_name and python_version do
// not answer for name and version.
var (
	gradleVersionLine     = regexp.MustCompile(`(?m)^[ \t]*version\s*=\s*['"]([^'"\s]+)['"]`)
	gradleRootProjectLine = regexp.MustCompile(`(?m)rootProject\.name\s*=\s*['"]?([^'"\s]+)`)
	pythonSetupName       = regexp.MustCompile(`\bname\s*=\s*['"]([^'"]+)['"]`)
	pythonSetupVersion    = regexp.MustCompile(`\bversion\s*=\s*['"]([^'"]+)['"]`)
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
	path := goModulePath(body)
	if path == "" {
		return ""
	}

	return filepath.Base(path)
}

// GoModuleMajorVersion returns the synthetic major-version string
// (e.g. "2.0.0" when the module path ends in `/v2`). Mirrors the
// `s/v\K[0-9]+/$&.0.0/` bash logic.
func GoModuleMajorVersion(body []byte) string {
	path := goModulePath(body)
	if path == "" {
		return ""
	}

	_, pathMajor, ok := module.SplitPathVersion(path)
	if !ok || pathMajor == "" {
		return ""
	}

	major := strings.TrimPrefix(pathMajor, "/v")

	major = strings.TrimPrefix(major, ".v")
	if major == pathMajor {
		return ""
	}

	return major + ".0.0"
}

func goModulePath(body []byte) string {
	parsed, err := modfile.Parse("go.mod", body, nil)
	if err != nil || parsed.Module == nil {
		return ""
	}

	return strings.TrimSpace(parsed.Module.Mod.Path)
}

type tomlIdentity struct {
	Name    string
	Version string
}

type tomlIdentityTable struct {
	Name    any `toml:"name"`
	Version any `toml:"version"`
}

type cargoTOMLDocument struct {
	Package   tomlIdentityTable `toml:"package"`
	Workspace struct {
		Package tomlIdentityTable `toml:"package"`
	} `toml:"workspace"`
}

// ErrCargoWorkspaceVersionUnavailable means package.version is inherited but
// the workspace root did not provide a usable workspace.package.version.
var ErrCargoWorkspaceVersionUnavailable = errors.New("cargo workspace package version unavailable")

func decodeTOMLIdentity(body []byte, table string) tomlIdentity {
	var document struct {
		Package tomlIdentityTable `toml:"package"`
		Project tomlIdentityTable `toml:"project"`
	}
	if err := toml.Unmarshal(body, &document); err != nil {
		return tomlIdentity{}
	}

	selected := document.Project
	if table == "package" {
		selected = document.Package
	}

	name, _ := selected.Name.(string)
	version, _ := selected.Version.(string)

	return tomlIdentity{Name: name, Version: version}
}

// CargoTOMLName returns package.name from a Cargo.toml body.
func CargoTOMLName(body []byte) string {
	return decodeTOMLIdentity(body, "package").Name
}

// CargoTOMLVersion returns package.version from a Cargo.toml body, resolving
// version.workspace through the workspace root Cargo.toml when supplied.
func CargoTOMLVersion(body, workspaceBody []byte) (string, error) {
	var document cargoTOMLDocument
	if err := toml.Unmarshal(body, &document); err != nil {
		return "", nil //nolint:nilerr // Non-inheritance parser failures retain the existing unknown-version fallback.
	}

	if version, ok := document.Package.Version.(string); ok {
		return version, nil
	}

	inheritance, ok := document.Package.Version.(map[string]any)

	workspace, inherited := inheritance["workspace"].(bool)
	if !ok || !inherited || !workspace {
		return "", nil
	}

	if len(workspaceBody) == 0 {
		return "", fmt.Errorf("%w: package.version uses workspace = true; provide the workspace root Cargo.toml or set package.version explicitly", ErrCargoWorkspaceVersionUnavailable)
	}

	var workspaceDocument cargoTOMLDocument
	if err := toml.Unmarshal(workspaceBody, &workspaceDocument); err != nil {
		return "", fmt.Errorf("%w: parse workspace root Cargo.toml: %w", ErrCargoWorkspaceVersionUnavailable, err)
	}

	if version, ok := workspaceDocument.Workspace.Package.Version.(string); ok && version != "" {
		return version, nil
	}

	return "", fmt.Errorf("%w: package.version uses workspace = true but [workspace.package].version is missing or not a string; define it in the workspace root Cargo.toml or set package.version explicitly", ErrCargoWorkspaceVersionUnavailable)
}

// PyProjectName returns project.name from a pyproject.toml body.
func PyProjectName(body []byte) string {
	return decodeTOMLIdentity(body, "project").Name
}

// PyProjectVersion returns project.version from a pyproject.toml body.
func PyProjectVersion(body []byte) string {
	return decodeTOMLIdentity(body, "project").Version
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
