// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package syncguard holds the guards that keep a generated file in step with
// the Go declarations it is generated from.
//
// docs/cli-reference.md, docs/artifacts-reference.md,
// .reusable-ci/artifacts.schema.json, and
// docs/schemas/release-images.schema.json are all committed copies of
// something the code already knows. A committed copy is a claim about the
// code, and an unchecked claim goes stale on the first change that forgets
// it -- which for a published JSON Schema means an adopter validating against
// a contract the binary no longer implements.
//
// Each guard regenerates the artifact in memory and compares, so the failure
// names the refresh command rather than the diff.
//
// They live here rather than in internal/cli, where they were originally
// written, because they read docs/ and .reusable-ci/ and have nothing to say
// about the CLI command surface. See also internal/archguard (import
// direction), internal/lexiconguard (single-sourced literals), and
// internal/workflowguard (the workflow contract).
package syncguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
)

// TestArtifactsReferenceDocumentsEverySchemaEnum guards
// docs/artifacts-reference.md against silent drift from the config schema.
//
// Unlike docs/cli-reference.md and .reusable-ci/artifacts.schema.json — both
// generated from Go and byte-checked by TestDocsCLIReferenceInSync /
// TestArtifactsSchemaInSync — the artifacts reference is hand-written prose,
// so it has no generator to keep it honest. This is its drift gate: every
// closed enum value the schema recognises must appear, backtick-quoted, in the
// reference. Adding a project type, publish target, SBOM layer, build type or
// build mode without documenting it fails here.
//
// Scope is the closed enums (the high-churn part of the schema). On failure,
// document the new value in docs/artifacts-reference.md by hand — there is no
// generator to run.
func TestArtifactsReferenceDocumentsEverySchemaEnum(t *testing.T) {
	t.Parallel()

	path := filepath.Join(reporoot.Path(t), "docs", "artifacts-reference.md")
	body, err := os.ReadFile(path) //nolint:gosec // test reads repo-local docs file.
	require.NoErrorf(t, err, "read %s", path)

	doc := string(body)

	for _, e := range schemaEnumValues() {
		require.Containsf(t, doc, "`"+e.value+"`",
			"docs/artifacts-reference.md does not document the %s value %q "+
				"(expected it backtick-quoted); document new schema enum values "+
				"there by hand — the reference is not generated",
			e.group, e.value)
	}
}

type schemaEnum struct {
	group string
	value string
}

// schemaEnumValues flattens every closed schema enum to (group, value) pairs,
// reading from the same Go declarations the JSON schema renders from so the
// two never disagree about what the enum set is.
func schemaEnumValues() []schemaEnum {
	out := make([]schemaEnum, 0,
		len(config.ValidProjectTypes)+len(config.ValidBuildTypes)+
			len(config.ValidPublishTargets)+len(config.ValidSBOMLayers)+4)

	for _, v := range config.ValidProjectTypes {
		out = append(out, schemaEnum{"project-type", string(v)})
	}

	for _, v := range config.ValidBuildTypes {
		out = append(out, schemaEnum{"build-type", string(v)})
	}

	for _, v := range config.ValidPublishTargets {
		out = append(out, schemaEnum{"publish-to", string(v)})
	}

	for _, v := range config.ValidSBOMLayers {
		out = append(out, schemaEnum{"sboms layer", string(v)})
	}

	// Go and Cargo build modes share the same two tokens; a set dedupes them.
	for _, v := range []string{
		string(config.GoBuildModeArtifactFirst),
		string(config.GoBuildModeContainerFirst),
		string(config.CargoBuildModeArtifactFirst),
		string(config.CargoBuildModeContainerFirst),
	} {
		out = append(out, schemaEnum{"build-mode", v})
	}

	return out
}
