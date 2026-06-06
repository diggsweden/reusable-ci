// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package projecttype owns the canonical enum of project-type strings
// used across the binary: artifacts.yml schema, sbom layer dispatch,
// version-bump dispatch, summary rendering.
//
// Single source of truth: every value is declared here. Per-context
// "which subset is valid here" lists stay in the consuming packages
// (config.ValidProjectTypes, sbom.ValidProjectTypes) because the
// subsets are context-specific and shouldn't pollute the canonical
// enum.
//
// Naming follows Go's "package qualifies short names" convention:
// `projecttype.Maven`, not `projecttype.ProjectMaven`.
package projecttype

import (
	"fmt"
	"strings"
)

// Type is the canonical project-type enum.
type Type string

// Recognised Type values. Maven through Cargo are buildable project types
// detected from manifest files; Auto, Meta and Unknown are sentinels.
const (
	// Auto requests filesystem-based detection (DetectFromEntries).
	// Valid as an input to `sbom generate` only.
	Auto Type = "auto"

	Maven         Type = "maven"
	NPM           Type = "npm"
	Gradle        Type = "gradle"
	GradleAndroid Type = "gradle-android"
	XcodeIOS      Type = "xcode-ios"
	Python        Type = "python"
	Go            Type = "go"
	Cargo         Type = "cargo"

	// Meta is a non-buildable / changelog-only artefact. Valid in
	// artifacts.yml as an explicit declaration that no version file
	// exists and no SBOM is generated.
	Meta Type = "meta"

	// Unknown is the result of failed auto-detection. Not valid as an
	// input — only as the zero-value sentinel returned by
	// DetectFromEntries when nothing matched.
	Unknown Type = "unknown"
)

// String renders the type as its underlying string. Implements fmt.Stringer.
func (t Type) String() string { return string(t) }

// DetectFromEntries returns the project type implied by the set of
// manifest filenames in entries. Mirrors detect_project_type in the
// bash: stops at the first match in the priority order
// pom.xml → package.json → build.gradle[.kts] → go.mod → Cargo.toml →
// pyproject.toml/requirements.txt/setup.py.
//
// entries is a slice of plain basenames (no directory prefix).
// Returns Unknown when nothing matches.
func DetectFromEntries(entries []string) Type {
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		set[e] = struct{}{}
	}

	switch {
	case has(set, "pom.xml"):
		return Maven
	case has(set, "package.json"):
		return NPM
	case has(set, "build.gradle"), has(set, "build.gradle.kts"):
		return Gradle
	case has(set, "go.mod"):
		return Go
	case has(set, "Cargo.toml"):
		return Cargo
	case has(set, "pyproject.toml"), has(set, "requirements.txt"), has(set, "setup.py"):
		return Python
	}

	return Unknown
}

func has(set map[string]struct{}, key string) bool {
	_, ok := set[key]

	return ok
}

// IsIn reports whether t is in the given list. Sugar for the common
// "is this type in this context's accepted subset?" check.
func IsIn(t Type, list []Type) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}

	return false
}

// UnknownTypeError is returned by Parse when the input string isn't a known type.
type UnknownTypeError struct {
	Input string
	Valid []Type
}

func (e *UnknownTypeError) Error() string {
	names := make([]string, len(e.Valid))
	for i, v := range e.Valid {
		names[i] = string(v)
	}

	return fmt.Sprintf("unknown project type %q (valid: %s)", e.Input, strings.Join(names, ", "))
}
