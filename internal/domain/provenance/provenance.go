// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package provenance builds an in-toto Statement v1 carrying a SLSA
// Provenance v1.0 predicate. It is pure: no env reads, no git, no I/O,
// no signing — callers resolve every value (subjects, builder id,
// timestamps, the forge-specific Profile) and hand them in, so the
// predicate shape is golden-testable in isolation. Signing the result
// (cosign attest-blob / sign-blob) is a separate concern.
//
// Ported from forgejo-ci's scripts/release/slsa-provenance.sh; the JSON
// shape is kept byte-compatible with that generator.
//
// References:
//   - https://slsa.dev/spec/v1.0/provenance
//   - https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md
package provenance

import (
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// Subject is one artifact attested by the statement: a name and its
// SHA-256 hex digest.
type Subject struct {
	Name   string
	SHA256 string
}

// Dependency is one resolvedDependencies entry: a package URI and a
// single named digest (e.g. "gitCommit" for the source, "gomod_h1" for
// a Go module — naming the digest type tells verifiers exactly what to
// compare against).
type Dependency struct {
	URI        string
	DigestType string
	Digest     string
}

// Profile carries the forge-specific provenance vocabulary so the pure
// builder stays provider-agnostic. Adapters supply it (see
// provider.ProvenanceProfiler).
type Profile struct {
	BuildType         string // predicate.buildDefinition.buildType URI
	WorkflowDirPrefix string // e.g. ".forgejo/workflows/" or ".github/workflows/"
	RunnerLabel       string // internalParameters.runner, e.g. "forgejo-actions"
}

// Input is everything Build needs. All values are pre-resolved.
type Input struct {
	Subjects      []Subject
	RepositoryURL string // <server>/<owner>/<repo>
	Ref           string // tag, e.g. v1.2.3
	WorkflowFile  string // workflow filename, e.g. release.yml
	BuilderID     string // <repo-url>/<workflow-dir><workflow>@<ref>
	InvocationID  string // <repo-url>/actions/runs/<run-id>
	StartedOn     string // RFC3339 UTC; commit-derived for reproducibility
	FinishedOn    string // RFC3339 UTC
	Profile       Profile
	ResolvedDeps  []Dependency
}

// Statement is the in-toto Statement v1 envelope. Field order matches
// the JSON shape; encoding/json emits struct fields in declaration
// order and map keys sorted, so the output is deterministic.
type Statement struct {
	Type          string        `json:"_type"`
	Subject       []subjectJSON `json:"subject"`
	PredicateType string        `json:"predicateType"`
	Predicate     predicateJSON `json:"predicate"`
}

type subjectJSON struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type predicateJSON struct {
	BuildDefinition buildDefinitionJSON `json:"buildDefinition"`
	RunDetails      runDetailsJSON      `json:"runDetails"`
}

type buildDefinitionJSON struct {
	BuildType            string             `json:"buildType"`
	ExternalParameters   externalParamsJSON `json:"externalParameters"`
	InternalParameters   internalParamsJSON `json:"internalParameters"`
	ResolvedDependencies []resolvedDepJSON  `json:"resolvedDependencies"`
}

type externalParamsJSON struct {
	Workflow workflowRefJSON `json:"workflow"`
}

type workflowRefJSON struct {
	Ref        string `json:"ref"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
}

type internalParamsJSON struct {
	Runner string `json:"runner"`
}

type resolvedDepJSON struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

type runDetailsJSON struct {
	Builder  builderJSON  `json:"builder"`
	Metadata metadataJSON `json:"metadata"`
}

type builderJSON struct {
	ID string `json:"id"`
}

type metadataJSON struct {
	InvocationID string `json:"invocationId"`
	StartedOn    string `json:"startedOn"`
	FinishedOn   string `json:"finishedOn"`
}

const (
	statementType = "https://in-toto.io/Statement/v1"
	predicateType = "https://slsa.dev/provenance/v1"
)

// Build assembles the in-toto Statement from a fully-resolved Input.
// It validates the minimum honesty invariants — at least one subject and
// the required identifying fields — so a provenance with missing context
// is refused rather than silently emitted.
func Build(in Input) (Statement, error) {
	if len(in.Subjects) == 0 {
		return Statement{}, fmt.Errorf("provenance: no subjects: %w", errs.ErrUsage)
	}

	for _, miss := range []struct {
		name string
		val  string
	}{
		{"RepositoryURL", in.RepositoryURL},
		{"Ref", in.Ref},
		{"WorkflowFile", in.WorkflowFile},
		{"BuilderID", in.BuilderID},
	} {
		if miss.val == "" {
			return Statement{}, fmt.Errorf("provenance: %s is required: %w", miss.name, errs.ErrUsage)
		}
	}

	subjects := make([]subjectJSON, 0, len(in.Subjects))
	for _, s := range in.Subjects {
		subjects = append(subjects, subjectJSON{Name: s.Name, Digest: map[string]string{"sha256": s.SHA256}})
	}

	deps := make([]resolvedDepJSON, 0, len(in.ResolvedDeps))
	for _, d := range in.ResolvedDeps {
		deps = append(deps, resolvedDepJSON{URI: d.URI, Digest: map[string]string{d.DigestType: d.Digest}})
	}

	return Statement{
		Type:          statementType,
		Subject:       subjects,
		PredicateType: predicateType,
		Predicate: predicateJSON{
			BuildDefinition: buildDefinitionJSON{
				BuildType: in.Profile.BuildType,
				ExternalParameters: externalParamsJSON{
					Workflow: workflowRefJSON{
						Ref:        in.Ref,
						Repository: in.RepositoryURL,
						Path:       in.Profile.WorkflowDirPrefix + in.WorkflowFile,
					},
				},
				InternalParameters:   internalParamsJSON{Runner: in.Profile.RunnerLabel},
				ResolvedDependencies: deps,
			},
			RunDetails: runDetailsJSON{
				Builder: builderJSON{ID: in.BuilderID},
				Metadata: metadataJSON{
					InvocationID: in.InvocationID,
					StartedOn:    in.StartedOn,
					FinishedOn:   in.FinishedOn,
				},
			},
		},
	}, nil
}

// JSON renders the statement as indented JSON with a trailing newline,
// matching the slsa-provenance.sh output (jq-pretty + newline).
func (s Statement) JSON() ([]byte, error) {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal statement: %w", err)
	}

	return append(body, '\n'), nil
}
