// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestValidate_HappyPath(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "my-app", ProjectType: projecttype.Maven},
		},
	}
	if err := config.Validate(c); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidate_RejectsEmptyArtifacts(t *testing.T) {
	t.Parallel()

	err := config.Validate(&config.Config{})
	if !config.IsValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if !strings.Contains(err.Error(), "no artifacts") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_RejectsInvalidProjectType(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: "rust"}, // not in ValidProjectTypes
		},
	}
	err := config.Validate(c)
	if !config.IsValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if !strings.Contains(err.Error(), "invalid projectType") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_AcceptsAllKnownTypes(t *testing.T) {
	t.Parallel()

	for _, pt := range config.ValidProjectTypes {

		t.Run(string(pt), func(t *testing.T) {
			t.Parallel()
			c := &config.Config{
				Artifacts: []config.Artifact{{Name: "a", ProjectType: pt}},
			}
			if err := config.Validate(c); err != nil {
				t.Errorf("project-type %q rejected: %v", pt, err)
			}
		})
	}
}

func TestValidate_RejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: projecttype.Maven},
			{Name: "x", ProjectType: projecttype.NPM},
		},
	}
	err := config.Validate(c)
	if !config.IsValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("err message: %v", err)
	}
}

func TestValidate_RejectsEmptyArtifactName(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "", ProjectType: projecttype.Maven},
		},
	}
	err := config.Validate(c)
	if !config.IsValidationError(err) {
		t.Errorf("err = %v, want ValidationError", err)
	}
}

func TestValidate_ContainerReferencesUnknownArtifact(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "real", ProjectType: projecttype.Maven},
		},
		Containers: []config.Container{
			{Name: "ct", From: []string{"real", "missing"}},
		},
	}
	err := config.Validate(c)
	if !config.IsValidationError(err) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("err should mention missing artefact: %v", err)
	}
}

func TestValidate_ReportsAllViolations(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "x", ProjectType: "rust"},
			{Name: "x", ProjectType: projecttype.Maven}, // dup
		},
		Containers: []config.Container{
			{Name: "ct", From: []string{"missing"}},
		},
	}
	err := config.Validate(c)
	if err == nil {
		t.Fatal("expected error")
	}
	ve := func() *config.ValidationError {
		target := &config.ValidationError{}
		_ = errors.As(err, &target)
		return target
	}()
	if len(ve.Violations) < 3 {
		t.Errorf("expected ≥3 violations, got %d: %v", len(ve.Violations), ve.Violations)
	}
}

func TestWarnings_MavenAppToGitHubPackages(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name: "app", ProjectType: projecttype.Maven,
				BuildType: "application",
				PublishTo: []config.PublishTarget{config.PublishGitHubPackages},
			},
		},
	}
	w := config.Warnings(c)
	if len(w) != 1 || !strings.Contains(w[0], "Maven application") {
		t.Errorf("Warnings = %v, want 1 maven-app warning", w)
	}
}

func TestWarnings_LibraryToGitHubPackagesIsOK(t *testing.T) {
	t.Parallel()

	c := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name: "lib", ProjectType: projecttype.Maven,
				BuildType: "library",
				PublishTo: []config.PublishTarget{config.PublishGitHubPackages},
			},
		},
	}
	if w := config.Warnings(c); len(w) != 0 {
		t.Errorf("Warnings = %v, want none for library", w)
	}
}
