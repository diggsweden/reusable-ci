// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
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
	// V2+ modules: the bash returns the basename of the module path,
	// which is the version suffix ("v2"). Faithfully mirrored — fixing
	// this is a separate concern from porting.
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

	body := []byte(`[package]
name = "demo"
version = "1.2.3"
`)
	require.Equal(t, "demo", sbom.CargoTOMLName(body))
	require.Equal(t, "1.2.3", sbom.CargoTOMLVersion(body))
}

func TestPython_PyProjectAndSetupPy(t *testing.T) {
	t.Parallel()

	py := []byte(`[project]
name = "demo"
version = "1.2.3"
`)
	require.Equal(t, "demo", sbom.PyProjectName(py))
	require.Equal(t, "1.2.3", sbom.PyProjectVersion(py))

	setup := []byte(`setup(name='demo', version='1.2.3')`)
	require.Equal(t, "demo", sbom.SetupPyName(setup))
	require.Equal(t, "1.2.3", sbom.SetupPyVersion(setup))
}
