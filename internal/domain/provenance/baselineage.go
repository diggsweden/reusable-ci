// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var sha256HexRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BaseLineageInput drives BaseLineagePredicate. It preserves forgejo-ci's
// shipped base-image lineage predicate contract while still emitting standard
// SLSA Provenance v1.0.
type BaseLineageInput struct {
	Source      string
	Commit      string
	Workflow    string
	Flavor      string
	BaseInputID string
	Image       string
	BuildType   string
	BuilderID   string
}

type baseLineagePredicateJSON struct {
	BuildDefinition baseLineageBuildDefinitionJSON `json:"buildDefinition"`
	RunDetails      baseLineageRunDetailsJSON      `json:"runDetails"`
}

type baseLineageBuildDefinitionJSON struct {
	BuildType            string                              `json:"buildType"`
	ExternalParameters   baseLineageExternalParametersJSON   `json:"externalParameters"`
	InternalParameters   baseLineageInternalParametersJSON   `json:"internalParameters"`
	ResolvedDependencies []baseLineageResolvedDependencyJSON `json:"resolvedDependencies"`
}

type baseLineageExternalParametersJSON struct {
	Source      string `json:"source"`
	Workflow    string `json:"workflow"`
	Flavor      string `json:"flavor"`
	BaseInputID string `json:"base_input_id"`
	Image       string `json:"image"`
}

type baseLineageInternalParametersJSON struct {
	Runner string `json:"runner"`
}

type baseLineageResolvedDependencyJSON struct {
	URI    string                              `json:"uri"`
	Digest baseLineageResolvedDependencyDigest `json:"digest"`
}

type baseLineageResolvedDependencyDigest struct {
	GitCommit string `json:"gitCommit"`
}

type baseLineageRunDetailsJSON struct {
	Builder  builderJSON       `json:"builder"`
	Metadata map[string]string `json:"metadata"`
}

// BaseLineagePredicate renders the exact SLSA Provenance v1.0 predicate shape
// used by forgejo-ci for base-image lineage attestations. It is a bare
// predicate, not an in-toto statement; cosign binds the OCI image subject.
func BaseLineagePredicate(in BaseLineageInput) ([]byte, error) {
	for _, miss := range []struct {
		name  string
		value string
	}{
		{"source", in.Source},
		{"commit", in.Commit},
		{"workflow", in.Workflow},
		{"flavor", in.Flavor},
		{"base-input-id", in.BaseInputID},
		{"image", in.Image},
		{"build-type", in.BuildType},
	} {
		if miss.value == "" {
			return nil, fmt.Errorf("base lineage predicate: --%s is required: %w", miss.name, errs.ErrUsage)
		}
	}

	if !sha256HexRE.MatchString(in.BaseInputID) {
		return nil, fmt.Errorf("base lineage predicate: --base-input-id must be a sha256 hex digest: %w", errs.ErrUsage)
	}

	builderID := in.BuilderID
	if builderID == "" {
		builderID = in.Source + "/.forgejo/workflows/" + in.Workflow + "@" + in.Commit
	}

	body, err := json.MarshalIndent(baseLineagePredicateJSON{
		BuildDefinition: baseLineageBuildDefinitionJSON{
			BuildType: in.BuildType,
			ExternalParameters: baseLineageExternalParametersJSON{
				Source:      in.Source,
				Workflow:    in.Workflow,
				Flavor:      in.Flavor,
				BaseInputID: in.BaseInputID,
				Image:       in.Image,
			},
			InternalParameters: baseLineageInternalParametersJSON{Runner: "forgejo-actions"},
			ResolvedDependencies: []baseLineageResolvedDependencyJSON{{
				URI:    "git+" + in.Source + "@" + in.Commit,
				Digest: baseLineageResolvedDependencyDigest{GitCommit: in.Commit},
			}},
		},
		RunDetails: baseLineageRunDetailsJSON{
			Builder:  builderJSON{ID: builderID},
			Metadata: map[string]string{},
		},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("base lineage predicate: marshal: %w", err)
	}

	return append(body, '\n'), nil
}
