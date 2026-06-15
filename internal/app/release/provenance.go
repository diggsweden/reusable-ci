// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/provenance"
)

// ProvenanceInput drives GenerateProvenance. The CLI resolves the
// context (provider EventContext, runner workflow/run-id, reproducible
// timestamp, forge profile) and opens the readers; this use case is pure
// orchestration over the provenance domain so it stays testable without
// env, git, or the network.
type ProvenanceInput struct {
	Checksums     io.Reader // required: GoReleaser-format checksums
	GoSum         io.Reader // optional: go.sum for resolved module deps (nil → none)
	RepositoryURL string    // <server>/<owner>/<repo>
	Ref           string    // tag, e.g. v1.2.3
	SHA           string    // commit SHA
	WorkflowFile  string    // workflow filename, e.g. release.yml
	RunID         string    // Actions run id
	StartedOn     string    // RFC3339 UTC; commit-derived for reproducibility
	Profile       provenance.Profile
}

// GenerateProvenance parses the checksums (+ optional go.sum) and builds
// the signed-ready in-toto Statement JSON. Builder/invocation/source
// identifiers are derived from the repository URL, profile, workflow, and
// ref so the predicate is honest and reproducible.
func GenerateProvenance(in ProvenanceInput) ([]byte, error) {
	subjects, err := provenance.ParseChecksums(in.Checksums)
	if err != nil {
		return nil, err
	}

	deps := []provenance.Dependency{provenance.SourceDependency(in.RepositoryURL, in.Ref, in.SHA)}

	if in.GoSum != nil {
		mods, perr := provenance.ParseGoSum(in.GoSum)
		if perr != nil {
			return nil, perr
		}

		deps = append(deps, mods...)
	}

	builderID := in.RepositoryURL + "/" + in.Profile.WorkflowDirPrefix + in.WorkflowFile + "@" + in.Ref
	invocationID := in.RepositoryURL + "/actions/runs/" + in.RunID

	stmt, err := provenance.Build(provenance.Input{
		Subjects:      subjects,
		RepositoryURL: in.RepositoryURL,
		Ref:           in.Ref,
		WorkflowFile:  in.WorkflowFile,
		BuilderID:     builderID,
		InvocationID:  invocationID,
		StartedOn:     in.StartedOn,
		FinishedOn:    in.StartedOn,
		Profile:       in.Profile,
		ResolvedDeps:  deps,
	})
	if err != nil {
		return nil, err
	}

	return stmt.JSON()
}
