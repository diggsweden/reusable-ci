// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	_ "embed"
	"strings"

	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// artifactsSchemaTemplate is the authored JSON Schema for artifacts.yml
// with every enum and derived pattern left as a {{PLACEHOLDER}}. The
// Go declarations in this package (and domain/release for sign
// methods) are the single source of those value sets; the rendered
// schema at .reusable-ci/artifacts.schema.json is generated output —
// run `just gen-artifacts-schema` after changing either side. The
// byte-equality sync test in internal/cli gates drift.
//
//go:embed artifacts.schema.json.tmpl
var artifactsSchemaTemplate string

// RenderArtifactsJSONSchema renders the artifacts.yml JSON Schema from
// the template, injecting every enum and derived pattern from the Go
// schema declarations so the published schema cannot drift from what
// Parse/Validate actually accept.
func RenderArtifactsJSONSchema() string {
	return strings.NewReplacer(
		"{{PROJECT_TYPES}}", enumJSON(ValidProjectTypes),
		"{{BUILD_TYPES}}", enumJSON(ValidBuildTypes),
		"{{PUBLISH_TARGETS}}", enumJSON(ValidPublishTargets),
		"{{SIGN_METHODS}}", enumJSON(domainrelease.ValidSignMethods),
		"{{GIT_SIGN_METHODS}}", enumJSON(ValidGitSignMethods),
		"{{SBOM_PATTERN}}", sbomsPattern(),
		"{{KMS_KEY_PATTERN}}", kmsKeyPattern(),
	).Replace(artifactsSchemaTemplate)
}

// enumJSON renders an ordered enum slice as a one-line JSON string
// array: ["a", "b"]. None of the enum values need JSON escaping —
// they are all short lowercase identifiers.
func enumJSON[T ~string](values []T) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = `"` + string(value) + `"`
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

// sbomsPattern derives the `sboms` value regex from ValidSBOMLayers:
// "all", "none", or a comma-separated subset of the layer tokens.
// The "all"/"none" keywords are grammar of ExpandSBOMs in sboms.go.
func sbomsPattern() string {
	layers := make([]string, len(ValidSBOMLayers))
	for i, layer := range ValidSBOMLayers {
		layers[i] = string(layer)
	}

	alternation := strings.Join(layers, "|")

	return "^(all|none|((" + alternation + ")(,(" + alternation + "))*))$"
}

// kmsKeyPattern derives the sign.key scheme allowlist regex from
// allowedKMSSchemes, the same list Validate enforces.
func kmsKeyPattern() string {
	return "^(" + strings.Join(allowedKMSSchemes, "|") + "):"
}
