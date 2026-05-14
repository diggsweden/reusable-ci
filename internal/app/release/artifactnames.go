// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

// ResolveArtifactNamesInput drives `release resolve-artifact-name`.
type ResolveArtifactNamesInput struct {
	ProjectType  string
	ArtifactName string
}

// ResolveArtifactNames prints `name=…` and `sbom-name=…` to stdout —
// matching the bash's GitHub-Actions output format. (The bash also
// writes to GITHUB_OUTPUT via redirection of stdout; the Go binary
// keeps the same behaviour by relying on the calling workflow to
// redirect.)
func ResolveArtifactNames(_ context.Context, out io.Writer, in ResolveArtifactNamesInput) error {
	if in.ProjectType == "" {
		return fmt.Errorf("Usage: resolve-artifact-name <project-type>: %w", errs.ErrUsage)
	}
	pair := release.ResolveArtifactNames(projecttype.Type(in.ProjectType), in.ArtifactName)
	fmt.Fprintf(out, "name=%s\n", pair.BuildArtifact)
	fmt.Fprintf(out, "sbom-name=%s\n", pair.SBOMArtifact)
	return nil
}

// ResolveMetadataInput drives `release resolve-release-metadata`.
type ResolveMetadataInput struct {
	Version      string
	Repository   string
	ArtifactName string
}

// ResolveMetadata writes version / version-no-v / project-name to the
// OutputSink. Surfaces a stderr line for the project-name resolution
// path (mirrors the bash's `printf … >&2`).
func ResolveMetadata(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in ResolveMetadataInput) error {
	if in.Version == "" {
		return fmt.Errorf("VERSION is required: %w", errs.ErrUsage)
	}
	if in.Repository == "" {
		return fmt.Errorf("REPOSITORY is required: %w", errs.ErrUsage)
	}
	projectName := release.ProjectNameFromRepo(in.ArtifactName, in.Repository)
	if in.ArtifactName != "" {
		fmt.Fprintf(stderr, "Using artifact name: %s\n", projectName)
	} else {
		fmt.Fprintf(stderr, "Using repository name: %s\n", projectName)
	}
	if err := sink.Set(ctx, "version", in.Version); err != nil {
		return err
	}
	if err := sink.Set(ctx, "version-no-v", strings.TrimPrefix(in.Version, "v")); err != nil {
		return err
	}
	return sink.Set(ctx, "project-name", projectName)
}
