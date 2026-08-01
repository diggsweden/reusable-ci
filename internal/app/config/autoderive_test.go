// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"strings"
	"testing"
	"testing/fstest"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestAutoDerive_MavenRoot(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"pom.xml": &fstest.MapFile{Data: []byte(`<?xml version="1.0"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>se.digg.test</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
</project>`)},
	}

	cfg, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(cfg.Artifacts))
	}

	a := cfg.Artifacts[0]
	if a.Name != "my-app" {
		t.Errorf("name = %q, want my-app", a.Name)
	}

	if a.ProjectType != projecttype.Maven {
		t.Errorf("project-type = %q, want maven", a.ProjectType)
	}

	if a.WorkingDirectory != "." {
		t.Errorf("working-directory = %q, want .", a.WorkingDirectory)
	}
}

func TestAutoDerive_NPMRoot(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"@diggsweden/widget","version":"0.1.0"}`)},
	}

	cfg, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Scoped names strip the @scope/ prefix per PackageJSONName's contract.
	if cfg.Artifacts[0].Name != "widget" || cfg.Artifacts[0].ProjectType != projecttype.NPM {
		t.Errorf("artifact = %+v", cfg.Artifacts[0])
	}
}

func TestAutoDerive_CargoRoot(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"Cargo.toml": &fstest.MapFile{Data: []byte(`[package]
name = "my-crate"
version = "0.1.0"
edition = "2021"`)},
	}

	cfg, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Artifacts[0].Name != "my-crate" || cfg.Artifacts[0].Cargo == nil {
		t.Errorf("artifact = %+v", cfg.Artifacts[0])
	}
}

func TestAutoDerive_GoRoot(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module example.com/foo/bar\n\ngo 1.26\n")},
	}

	cfg, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// GoModuleName returns the basename of the module declaration.
	if cfg.Artifacts[0].Name != "bar" || cfg.Artifacts[0].Go == nil {
		t.Errorf("artifact = %+v", cfg.Artifacts[0])
	}
}

func TestAutoDerive_NoManifestErrorsActionably(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}

	_, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err == nil {
		t.Fatal("expected error for empty filesystem")
	}

	if !strings.Contains(err.Error(), "no recognised manifest") {
		t.Errorf("error should explain the actionable case; got: %v", err)
	}

	for _, want := range []string{"pom.xml", "package.json", "Cargo.toml", "go.mod"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list %s in the probe set; got: %v", want, err)
		}
	}
}

func TestAutoDerive_MultiManifestErrorsActionably(t *testing.T) {
	t.Parallel()

	// Polyglot repo — auto-derive refuses, demanding an explicit
	// artifacts.yml. The error must name every detected manifest so
	// the operator can either trim down or write the file.
	fsys := fstest.MapFS{
		"pom.xml":      &fstest.MapFile{Data: []byte("<project><groupId>x</groupId><artifactId>x</artifactId><version>1</version></project>")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x"}`)},
	}

	_, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err == nil {
		t.Fatal("expected error for multi-manifest repo")
	}

	if !strings.Contains(err.Error(), "multiple manifests") {
		t.Errorf("error should mention multi-manifest; got: %v", err)
	}

	for _, want := range []string{"pom.xml", "package.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list %s in the conflict; got: %v", want, err)
		}
	}
}

// TestAutoDerive_GradleBothScriptsCountedOnce verifies the dedup
// guarantee: a repo with both build.gradle and build.gradle.kts is
// still considered one Gradle project, not two.
func TestAutoDerive_GradleBothScriptsCountedOnce(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"build.gradle":     &fstest.MapFile{Data: []byte("plugins { id 'java' }")},
		"build.gradle.kts": &fstest.MapFile{Data: []byte("plugins { java }")},
		"settings.gradle":  &fstest.MapFile{Data: []byte(`rootProject.name = "myproj"`)},
	}

	cfg, err := appconfig.AutoDeriveConfig(fsys, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Artifacts[0].Name != "myproj" || cfg.Artifacts[0].ProjectType != projecttype.Gradle {
		t.Errorf("artifact = %+v", cfg.Artifacts[0])
	}
}
