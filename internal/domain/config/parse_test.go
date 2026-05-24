// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestParse_Maven(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: "25"
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := len(c.Artifacts); got != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", got)
	}

	a := c.Artifacts[0] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if a.Name != "my-app" || a.ProjectType != projecttype.Maven || a.BuildType != "application" {
		t.Errorf("artifact mismatch: %+v", a)
	}

	if a.Maven == nil || a.Maven.JavaVersion != "25" {
		t.Errorf("maven.java-version = %+v, want 25", a.Maven)
	}
}

func TestParse_PolyglotWithContainers(t *testing.T) {
	t.Parallel()

	in := []byte(`
artifacts:
  - name: backend
    project-type: maven
    publish-to: [maven-central]
  - name: frontend
    project-type: npm
containers:
  - name: my-app
    from: [backend, frontend]
    container-file: Containerfile
    enable-slsa: true
`)

	c, err := config.Parse(in) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(c.Artifacts) != 2 {
		t.Errorf("Artifacts = %d, want 2", len(c.Artifacts))
	}

	if len(c.Containers) != 1 {
		t.Errorf("Containers = %d, want 1", len(c.Containers))
	}

	if !c.Containers[0].EnableSLSAEffective() {
		t.Errorf("EnableSLSA not parsed")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := config.Parse(nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v, want substring 'empty'", err)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := config.Parse([]byte("not: valid: yaml: at: all: \n  - "))
	if err == nil {
		t.Errorf("expected parse error, got nil")
	}
}

func TestParse_PopulatesTypedSubStructForEachEcosystem(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: my-svc
    project-type: go
    config:
      build-mode: artifact-first
      binary-name: my-svc
      platforms: linux/amd64,linux/arm64
  - name: my-lib
    project-type: maven
    build-type: library
    config:
      java-version: "21"
      maven-profile: central-release
  - name: my-app
    project-type: gradle-android
    config:
      build-module: app
      build-types: debug,release
      include-aab: false
`)

	c, err := config.Parse(body) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	require.Len(t, c.Artifacts, 3)

	require.NotNil(t, c.Artifacts[0].Go)
	require.Equal(t, "my-svc", c.Artifacts[0].Go.BinaryName)
	require.Equal(t, "linux/amd64,linux/arm64", c.Artifacts[0].Go.Platforms)
	require.Equal(t, config.GoBuildModeArtifactFirst, c.Artifacts[0].Go.BuildMode)

	require.NotNil(t, c.Artifacts[1].Maven)
	require.Equal(t, "21", c.Artifacts[1].Maven.JavaVersion)
	require.Equal(t, "central-release", c.Artifacts[1].Maven.MavenProfile)

	require.NotNil(t, c.Artifacts[2].GradleAndroid)
	require.Equal(t, "app", c.Artifacts[2].GradleAndroid.BuildModule)
	require.NotNil(t, c.Artifacts[2].GradleAndroid.IncludeAAB)
	require.False(t, *c.Artifacts[2].GradleAndroid.IncludeAAB)
}

func TestParse_RejectsTypoInEcosystemConfig(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: my-app
    project-type: gradle-android
    config:
      build-module: app
      bogus-aab: false
`)

	_, err := config.Parse(body)
	if err == nil {
		t.Fatal("expected typo to be rejected, got nil")
	}

	if !strings.Contains(err.Error(), `"my-app"`) {
		t.Errorf("expected artifact name in error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "gradle-android") {
		t.Errorf("expected ecosystem in error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "bogus-aab") {
		t.Errorf("expected the offending key in error, got: %v", err)
	}
}

func TestParse_RejectsWrongTypedField(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: bad
    project-type: go
    config:
      skip-tests: "not a bool"
`)

	_, err := config.Parse(body)
	if err == nil {
		t.Fatal("expected type mismatch to be rejected")
	}
}

func TestParse_RejectsConfigOnMetaArtifact(t *testing.T) {
	t.Parallel()

	body := []byte(`
artifacts:
  - name: changelog-only
    project-type: meta
    config:
      anything: nope
`)

	_, err := config.Parse(body)
	if err == nil {
		t.Fatal("expected meta-with-config to be rejected")
	}

	if !strings.Contains(err.Error(), "meta") {
		t.Errorf("expected project-type in error, got: %v", err)
	}
}

func TestArtifact_AccessorsHaveSafeDefaults(t *testing.T) {
	t.Parallel()
	// Fabricated Artifact (no Parse). Accessors apply documented
	// defaults when the typed sub-struct is absent or its pointer-bool
	// is nil.
	a := config.Artifact{}
	require.True(t, a.IncludeAAB())
	require.True(t, a.EnableCodeSigning())
	require.Equal(t, "debug,release", a.AndroidBuildTypes())

	// Explicit-false overrides default.
	f := false
	a2 := config.Artifact{GradleAndroid: &config.GradleAndroidConfig{IncludeAAB: &f}}
	require.False(t, a2.IncludeAAB())

	a3 := config.Artifact{XcodeIOS: &config.XcodeIOSConfig{EnableCodeSigning: &f}}
	require.False(t, a3.EnableCodeSigning())
}
