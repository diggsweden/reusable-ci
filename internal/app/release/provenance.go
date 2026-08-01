// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// ProvenanceInput drives GenerateProvenance. The CLI resolves the context
// (provider EventContext, builder/invocation identifiers, reproducible
// timestamp) and opens the readers; this use case is pure orchestration over
// the forge-neutral provenance domain so it stays testable without env, git,
// or the network.
type ProvenanceInput struct {
	Checksums     io.Reader // required: GoReleaser-format checksums
	GoSum         io.Reader // optional: go.sum for resolved module deps (nil → none)
	RepositoryURL string    // <server>/<owner>/<repo>
	Ref           string    // tag, e.g. v1.2.3
	SHA           string    // commit SHA
	BuilderID     string    // forge-neutral build identity URI
	InvocationID  string    // forge-neutral run/job URL
	StartedOn     string    // RFC3339 UTC; commit-derived for reproducibility
}

// GenerateProvenance parses the checksums (+ optional go.sum) and builds the
// signed-ready in-toto Statement JSON. The predicate is forge-neutral
// (release-build buildType, {source,ref} external parameters) so it is
// byte-identical in shape to the container provenance.
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

	stmt, err := provenance.Build(provenance.Input{
		Subjects:     subjects,
		BuildType:    provenance.ReleaseBuildType,
		BuilderID:    in.BuilderID,
		SourceURI:    "git+" + in.RepositoryURL,
		Ref:          in.Ref,
		InvocationID: in.InvocationID,
		StartedOn:    in.StartedOn,
		FinishedOn:   in.StartedOn,
		ResolvedDeps: deps,
	})
	if err != nil {
		return nil, err
	}

	return stmt.JSON()
}
