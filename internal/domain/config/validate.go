// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
	"slices"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// ValidationError is returned when a Config fails one of the schema rules.
// Multiple violations are reported via Errors() so callers can show all of
// them rather than failing on the first.
type ValidationError struct {
	Violations []string
}

func (e *ValidationError) Error() string {
	if len(e.Violations) == 1 {
		return e.Violations[0]
	}
	return fmt.Sprintf("config: %d validation errors: %v", len(e.Violations), e.Violations)
}

// IsValidationError reports whether err is a *ValidationError.
func IsValidationError(err error) bool {
	if err == nil {
		return false
	}
	for err != nil {
		validationError := &ValidationError{}
		if errors.As(err, &validationError) {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Validate checks Config against the schema rules: artifacts list non-empty,
// every project-type recognised, container `from` references existing
// artefacts. Maven-application-to-github-packages combinations only emit a
// warning (returned via Warnings); they don't fail validation.
//
// Mirrors scripts/config/parse-artifacts-config.sh's validate_* functions.
func Validate(c *Config) error {
	if c == nil {
		return &ValidationError{Violations: []string{"config: nil Config"}}
	}

	var v []string

	if len(c.Artifacts) == 0 {
		v = append(v, "no artifacts found")
	}

	validTypes := make(map[projecttype.Type]bool, len(ValidProjectTypes))
	for _, t := range ValidProjectTypes {
		validTypes[t] = true
	}

	artifactNames := make(map[string]bool, len(c.Artifacts))
	for i, a := range c.Artifacts {
		switch {
		case a.Name == "":
			v = append(v, fmt.Sprintf("artifact #%d has empty name", i+1))
		case artifactNames[a.Name]:
			v = append(v, fmt.Sprintf("duplicate artifact name %q", a.Name))
		default:
			artifactNames[a.Name] = true
		}
		if !validTypes[a.ProjectType] {
			validList := projectTypesAsStrings()
			v = append(v, fmt.Sprintf(
				"invalid projectType %q for artifact %q (must be one of: %v)",
				a.ProjectType, a.Name, validList,
			))
		}
	}

	for _, ct := range c.Containers {
		if ct.Name == "" {
			v = append(v, "container with empty name")
		}
		for _, dep := range ct.From {
			if !artifactNames[dep] {
				v = append(v, fmt.Sprintf(
					"container %q references unknown artifact %q",
					ct.Name, dep,
				))
			}
		}
	}

	if len(v) > 0 {
		return &ValidationError{Violations: v}
	}
	return nil
}

// Warnings returns non-fatal observations about a Config: e.g. Maven
// applications publishing to github-packages (libraries should, but
// applications shouldn't). Validation passes regardless.
func Warnings(c *Config) []string {
	if c == nil {
		return nil
	}
	var w []string
	for _, a := range c.Artifacts {
		if a.ProjectType == projecttype.Maven &&
			a.BuildType == BuildTypeApplication &&
			slices.Contains(a.PublishTo, PublishGitHubPackages) {
			w = append(w, fmt.Sprintf(
				"Maven application %q publishing to github-packages — applications should not (libraries only)",
				a.Name,
			))
		}
	}
	return w
}

func projectTypesAsStrings() []string {
	out := make([]string, len(ValidProjectTypes))
	for i, t := range ValidProjectTypes {
		out[i] = string(t)
	}
	return out
}
