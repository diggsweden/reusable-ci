// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"strings"
	"testing"

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
      java-version: 25
`)
	c, err := config.Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(c.Artifacts); got != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", got)
	}
	a := c.Artifacts[0]
	if a.Name != "my-app" || a.ProjectType != projecttype.Maven || a.BuildType != "application" {
		t.Errorf("artifact mismatch: %+v", a)
	}
	if got, ok := a.Config["java-version"]; !ok || got != 25 {
		t.Errorf("config.java-version = %v (ok=%v), want 25", got, ok)
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
	c, err := config.Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Artifacts) != 2 {
		t.Errorf("Artifacts = %d, want 2", len(c.Artifacts))
	}
	if len(c.Containers) != 1 {
		t.Errorf("Containers = %d, want 1", len(c.Containers))
	}
	if !c.Containers[0].EnableSLSA {
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
