// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

// FuzzPackageJSON exercises the package.json name/version readers
// against arbitrary JSON-ish input. Both parsers must never panic
// (caller treats empty return as "couldn't determine").
func FuzzPackageJSON(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"name":"x"}`,
		`{"version":"1.2.3"}`,
		`{"name":"@scope/name","version":"1.0.0"}`,
		`{"name":"weird/slash/in/it","version":"1"}`,
		``,
		`not json`,
		"\x00\x01",
		strings.Repeat("{", 1000),
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		_ = sbom.PackageJSONName(body)
		_ = sbom.PackageJSONVersion(body)
	})
}

// FuzzGradleHelpers exercises the build.gradle / settings.gradle
// regex readers. The parsers must never panic; output for any
// non-matching input must be the empty string.
func FuzzGradleHelpers(f *testing.F) {
	seeds := []string{
		``,
		`version = '0.5.0'`,
		`version = "1.0"`,
		`rootProject.name = 'demo'`,
		`rootProject.name = "demo"`,
		"version = '\x00'",
		strings.Repeat("version = '0.5.0'\n", 100),
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		_ = sbom.GradleVersion(body)
		_ = sbom.GradleRootProjectName(body)
	})
}

// FuzzGoModHelpers exercises the go.mod parsers. Outputs must be
// stable strings (possibly empty); never panic on malformed input.
func FuzzGoModHelpers(f *testing.F) {
	seeds := []string{
		``,
		`module github.com/x/y`,
		`module github.com/x/y/v2`,
		"module x\n\ngo 1.24",
		"module\n",
		`module ` + strings.Repeat("a/", 500) + "v9",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		_ = sbom.GoModuleName(body)
		_ = sbom.GoModuleMajorVersion(body)
	})
}

// FuzzCargoTOML exercises the Cargo.toml name/version readers against
// arbitrary TOML-ish input.
func FuzzCargoTOML(f *testing.F) {
	seeds := []string{
		``,
		"[package]\nname = \"x\"\nversion = \"1.0\"",
		`name = "x"`,
		`version = "1.0"`,
		"name = \"\x00\"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		_ = sbom.CargoTOMLName(body)
		_ = sbom.CargoTOMLVersion(body)
	})
}

// FuzzPythonHelpers exercises pyproject.toml + setup.py readers.
func FuzzPythonHelpers(f *testing.F) {
	seeds := []string{
		``,
		"[project]\nname = \"x\"\nversion = \"1.0\"",
		`setup(name='x', version='1.0')`,
		`setup(name="x", version="1.0")`,
		"setup(\n  name = 'foo',\n  version='1.2.3',\n)",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		_ = sbom.PyProjectName(body)
		_ = sbom.PyProjectVersion(body)
		_ = sbom.SetupPyName(body)
		_ = sbom.SetupPyVersion(body)
	})
}
