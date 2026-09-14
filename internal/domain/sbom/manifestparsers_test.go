// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
)

func TestPackageJSON_ScopedNameStripped(t *testing.T) {
	t.Parallel()

	body := []byte(`{"name":"@digg/example","version":"1.2.3"}`)
	require.Equal(t, "example", sbom.PackageJSONName(body))
	require.Equal(t, "1.2.3", sbom.PackageJSONVersion(body))
}

func TestPackageJSON_PlainName(t *testing.T) {
	t.Parallel()

	plain := []byte(`{"name":"demo","version":"0.1.0"}`)
	require.Equal(t, "demo", sbom.PackageJSONName(plain))
}

func TestGradle_VersionAndRootProjectName(t *testing.T) {
	t.Parallel()

	build := []byte(`plugins { id 'java' }
version = '0.5.0'
group = 'se.digg'
`)
	require.Equal(t, "0.5.0", sbom.GradleVersion(build))

	settings := []byte(`rootProject.name = 'demo-app'`)
	require.Equal(t, "demo-app", sbom.GradleRootProjectName(settings))
}

func TestGoMod_V2PlusModule(t *testing.T) {
	t.Parallel()
	// GoModuleName currently uses the module-path basename, so a v2+
	// module resolves to its major-version suffix.
	mod := []byte(`module github.com/diggsweden/example/v2

go 1.24
`)
	require.Equal(t, "v2", sbom.GoModuleName(mod), "bash returns the v2 basename for v2+ modules")
	require.Equal(t, "2.0.0", sbom.GoModuleMajorVersion(mod))
}

func TestGoMod_V1Module(t *testing.T) {
	t.Parallel()

	mod := []byte(`module github.com/diggsweden/example

go 1.24
`)
	require.Empty(t, sbom.GoModuleMajorVersion(mod), "v1 modules yield no synthetic major version")
	require.Equal(t, "example", sbom.GoModuleName(mod))
}

func TestCargoTOML_NameAndVersion(t *testing.T) {
	t.Parallel()

	body := []byte(`[workspace.package]
name = "wrong-workspace"
version = "9.9.9"

[package]
name = 'demo'
version = '1.2.3'
`)
	require.Equal(t, "demo", sbom.CargoTOMLName(body))
	version, err := sbom.CargoTOMLVersion(body, body)
	require.NoError(t, err)
	require.Equal(t, "1.2.3", version)
}

func TestCargoTOML_ResolvesWorkspaceVersionWithoutHidingPackageName(t *testing.T) {
	t.Parallel()

	body := []byte(`[package]
name = "demo"
version = { workspace = true }

[workspace.package]
version = "2.3.4"
`)
	require.Equal(t, "demo", sbom.CargoTOMLName(body))
	version, err := sbom.CargoTOMLVersion(body, body)
	require.NoError(t, err)
	require.Equal(t, "2.3.4", version)
}

func TestCargoTOML_ResolvesVersionFromSeparateWorkspaceManifest(t *testing.T) {
	t.Parallel()

	member := []byte("[package]\nname = 'demo'\nversion = { workspace = true }\n")
	workspace := []byte("[workspace.package]\nversion = '3.4.5'\n")
	version, err := sbom.CargoTOMLVersion(member, workspace)
	require.NoError(t, err)
	require.Equal(t, "3.4.5", version)
}

func TestCargoTOML_WorkspaceVersionRequiresWorkspaceManifest(t *testing.T) {
	t.Parallel()

	member := []byte("[package]\nname = 'demo'\nversion = { workspace = true }\n")
	version, err := sbom.CargoTOMLVersion(member, nil)
	require.Empty(t, version)
	require.ErrorIs(t, err, sbom.ErrCargoWorkspaceVersionUnavailable)
	require.ErrorContains(t, err, "provide the workspace root Cargo.toml")
}

func TestPython_PyProjectAndSetupPy(t *testing.T) {
	t.Parallel()

	py := []byte(`[tool.poetry]
name = "wrong-tool"
version = "9.9.9"

[project]
name = 'demo'
version = '1.2.3'
`)
	require.Equal(t, "demo", sbom.PyProjectName(py))
	require.Equal(t, "1.2.3", sbom.PyProjectVersion(py))

	setup := []byte(`setup(name='demo', version='1.2.3')`)
	require.Equal(t, "demo", sbom.SetupPyName(setup))
	require.Equal(t, "1.2.3", sbom.SetupPyVersion(setup))
}

// TestManifestReaders_MatchTheProjectsOwnAssignment covers the identifiers
// the loose readers used to answer for. `ext.kotlin_version` before the
// project version, and `package_name`/`python_version` in a setup.py, each
// end in the word the reader wanted and were returned as it. A computed
// Gradle version is not a version at all.
func TestManifestReaders_MatchTheProjectsOwnAssignment(t *testing.T) {
	t.Parallel()

	gradle := []byte("buildscript {\n    ext.kotlin_version = '1.9.0'\n}\nplugins { id 'java' }\nversion = '2.0.0'\n")
	require.Equal(t, "2.0.0", sbom.GradleVersion(gradle))
	require.Equal(t, "2.0.0", sbom.GradleVersion([]byte("  version = \"2.0.0\"\n")))
	require.Empty(t, sbom.GradleVersion([]byte("version = project.findProperty('releaseVersion')\n")))
	require.Empty(t, sbom.GradleVersion([]byte("ext.kotlin_version = '1.9.0'\n")))

	setup := []byte("package_name = 'legacy_pkg'\nsetup(name='demo', python_version='3.9', version='1.2.3')\n")
	require.Equal(t, "demo", sbom.SetupPyName(setup))
	require.Equal(t, "1.2.3", sbom.SetupPyVersion(setup))
}

// TestGoMod_GopkgInMajorVersion: gopkg.in spells the major version as a
// ".vN" suffix rather than "/vN", and module.SplitPathVersion reports it the
// same way, so the synthetic version derives from both spellings.
func TestGoMod_GopkgInMajorVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, "3.0.0", sbom.GoModuleMajorVersion([]byte("module gopkg.in/yaml.v3\n\ngo 1.24\n")))
	// gopkg.in spells v1 explicitly, unlike the bare form, so it derives too.
	require.Equal(t, "1.0.0", sbom.GoModuleMajorVersion([]byte("module gopkg.in/check.v1\n")))
}

// TestCargoTOML_WorkspaceFalseIsNotInherited: `version.workspace = false`
// declares nothing inherited, so no workspace manifest is required and the
// version is simply unknown.
func TestCargoTOML_WorkspaceFalseIsNotInherited(t *testing.T) {
	t.Parallel()

	version, err := sbom.CargoTOMLVersion([]byte("[package]\nname = 'demo'\nversion = { workspace = false }\n"), nil)
	require.NoError(t, err)
	require.Empty(t, version)
}
