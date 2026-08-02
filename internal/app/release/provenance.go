// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// commitSHARE pins the source gitCommit shape: the digest is signed into the
// statement's resolvedDependencies, so a malformed value must fail here
// rather than become attested evidence.
var commitSHARE = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ProvenanceProfile selects the release statement shape. The empty profile is
// the generic reusable-ci profile; ForgejoActions preserves forgejo-ci's
// shipped release-blob provenance contract for existing downstream verifiers.
type ProvenanceProfile string

const (
	// ProvenanceProfileGeneric is the default forge-neutral release
	// provenance profile.
	ProvenanceProfileGeneric ProvenanceProfile = ""
	// ProvenanceProfileForgejoActions preserves forgejo-ci's shipped
	// release-blob provenance contract for existing downstream verifiers.
	ProvenanceProfileForgejoActions ProvenanceProfile = "forgejo-actions"
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
	Profile       ProvenanceProfile
	Workflow      string // forgejo-actions profile: workflow filename/path

	// ExternalParameters are caller-declared extra
	// buildDefinition.externalParameters. Computed keys are reserved —
	// a collision is ErrValidation, never an override.
	ExternalParameters map[string]any
}

// GenerateProvenance parses the checksums (+ optional go.sum) and builds the
// signed-ready in-toto Statement JSON. The default predicate is forge-neutral;
// the forgejo-actions profile is an explicit compatibility shape for existing
// forgejo-ci release verifiers.
func GenerateProvenance(in ProvenanceInput) ([]byte, error) {
	if !commitSHARE.MatchString(in.SHA) {
		return nil, fmt.Errorf("provenance: commit SHA must be a 40- or 64-character lowercase hex digest: %q: %w", in.SHA, errs.ErrValidation)
	}

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

	input := provenance.Input{
		Subjects:           subjects,
		BuildType:          provenance.ReleaseBuildType,
		BuilderID:          in.BuilderID,
		SourceURI:          "git+" + in.RepositoryURL,
		Ref:                in.Ref,
		InvocationID:       in.InvocationID,
		StartedOn:          in.StartedOn,
		FinishedOn:         in.StartedOn,
		ResolvedDeps:       deps,
		ExternalParameters: in.ExternalParameters,
	}

	switch in.Profile {
	case ProvenanceProfileGeneric:
	case ProvenanceProfileForgejoActions:
		if in.Workflow == "" {
			return nil, fmt.Errorf("provenance: --workflow is required for forgejo-actions profile: %w", errs.ErrUsage)
		}

		input.BuildType = provenance.ForgejoActionsWorkflowBuildType
		input.Workflow = &provenance.WorkflowExternalParameters{
			Ref:        in.Ref,
			Repository: in.RepositoryURL,
			Path:       forgejoActionsWorkflowPath(in.Workflow),
		}
		input.InternalParameters = map[string]string{"runner": "forgejo-actions"}
	default:
		return nil, fmt.Errorf("provenance: unknown profile %q: %w", in.Profile, errs.ErrUsage)
	}

	stmt, err := provenance.Build(input)
	if err != nil {
		return nil, err
	}

	return stmt.JSON()
}

func forgejoActionsWorkflowPath(workflow string) string {
	if strings.HasPrefix(workflow, ".forgejo/workflows/") {
		return workflow
	}

	return ".forgejo/workflows/" + workflow
}
