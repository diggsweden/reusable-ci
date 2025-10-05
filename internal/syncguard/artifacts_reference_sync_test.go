// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package syncguard keeps a committed file in step with the Go declarations it
// describes, whether it is generated wholesale or maintained by hand.
//
// docs/cli-reference.md, .reusable-ci/artifacts.schema.json and
// docs/schemas/release-images.schema.json are generated: the guard regenerates
// each in memory and compares, so a failure names the refresh command rather
// than printing a diff. docs/artifacts-reference.md is written by hand, so the
// guard compares each governed vocabulary block exactly and in both directions.
//
// Either way the committed file is a claim about the code, and an unchecked
// claim goes stale on the first change that forgets it. For a published JSON
// Schema that means an adopter validating against a contract the binary no
// longer implements.
//
// One of the repo-wide guard packages; docs/testing.md says which is which and
// where a new guard belongs.
package syncguard

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
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

	doc := string(reporoot.ReadFile(t, "docs/artifacts-reference.md"))

	groups := map[string][]string{}
	for _, entry := range schemaEnumValues() {
		groups[entry.group] = append(groups[entry.group], entry.value)
	}

	names := make([]string, 0, len(groups))
	for group := range groups {
		names = append(names, group)
	}

	require.ElementsMatch(t, []string{"project-type", "build-type", "publish-to", "sboms layer", "go build-mode", "cargo build-mode", "sign.method", "sign.transparency", "git-signing.method", "sign.key schemes"}, names)

	for group, want := range groups {
		got, err := documentedValues(doc, group)
		require.NoError(t, err)
		require.Equal(t, want, got, "docs/artifacts-reference.md family "+group)
	}
}

func documentedValues(doc, group string) ([]string, error) {
	start, end := "<!-- schema-values:"+group+" -->", "<!-- /schema-values:"+group+" -->"
	if strings.Count(doc, start) != 1 || strings.Count(doc, end) != 1 {
		return nil, fmt.Errorf("family %s needs exactly one documentation block: %w", group, errs.ErrValidation)
	}

	_, body, _ := strings.Cut(doc, start)

	body, _, ok := strings.Cut(body, end)
	if !ok {
		return nil, fmt.Errorf("family %s has reversed boundaries: %w", group, errs.ErrValidation)
	}

	var values []string
	for _, match := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(body, -1) {
		values = append(values, match[1])
	}

	return values, nil
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

	out = append(out, schemaEnum{"sboms layer", "all"}, schemaEnum{"sboms layer", "none"})
	for _, v := range config.ValidSBOMLayers {
		out = append(out, schemaEnum{"sboms layer", string(v)})
	}

	out = append(out, schemaEnum{"go build-mode", string(config.GoBuildModeArtifactFirst)}, schemaEnum{"go build-mode", string(config.GoBuildModeContainerFirst)}, schemaEnum{"cargo build-mode", string(config.CargoBuildModeArtifactFirst)}, schemaEnum{"cargo build-mode", string(config.CargoBuildModeContainerFirst)})
	for _, value := range release.ValidSignMethods {
		out = append(out, schemaEnum{"sign.method", string(value)})
	}

	for _, value := range release.ValidTransparencies {
		out = append(out, schemaEnum{"sign.transparency", string(value)})
	}

	for _, value := range config.ValidGitSignMethods {
		out = append(out, schemaEnum{"git-signing.method", string(value)})
	}

	for _, value := range config.KMSKeySchemes() {
		out = append(out, schemaEnum{"sign.key schemes", value})
	}

	return out
}
