// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

// ResolveArtifactNamesInput drives `release resolve-artifact-name`.
type ResolveArtifactNamesInput struct {
	ProjectType  string
	ArtifactName string
	Format       output.Format
	Sink         ci.OutputSink
}

// ResolveArtifactNames emits the canonical upload-artifact name pair.
func ResolveArtifactNames(ctx context.Context, out io.Writer, in ResolveArtifactNamesInput) error {
	if in.ProjectType == "" {
		return fmt.Errorf("project-type is required: pass --project-type <type> or set $PROJECT_TYPE: %w", errs.ErrUsage)
	}

	pair := release.ResolveArtifactNames(projecttype.Type(in.ProjectType), in.ArtifactName)
	if in.Sink != nil && (in.Format == output.FormatGitHub || in.Format == output.FormatGitLab) {
		if err := in.Sink.Set(ctx, "name", pair.BuildArtifact); err != nil {
			return err
		}

		if err := in.Sink.Set(ctx, "sbom-name", pair.SBOMArtifact); err != nil {
			return err
		}
	}

	if in.Format == output.FormatJSON {
		body, err := json.Marshal(struct {
			Name     string `json:"name"`
			SBOMName string `json:"sbom_name"`
		}{Name: pair.BuildArtifact, SBOMName: pair.SBOMArtifact})
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintln(out, string(body))

		return nil
	}

	_, _ = fmt.Fprintf(out, "name=%s\n", pair.BuildArtifact)
	_, _ = fmt.Fprintf(out, "sbom-name=%s\n", pair.SBOMArtifact)

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
// path.
func ResolveMetadata(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in ResolveMetadataInput) error {
	if in.Version == "" {
		return fmt.Errorf("version is required: pass --version <ver> or set $VERSION: %w", errs.ErrUsage)
	}

	if in.Repository == "" {
		return fmt.Errorf("repository is required: pass --repository <owner/repo> or set $REPOSITORY: %w", errs.ErrUsage)
	}

	projectName := release.ProjectNameFromRepo(in.ArtifactName, in.Repository)
	if in.ArtifactName != "" {
		_, _ = fmt.Fprintf(stderr, "Using artifact name: %s\n", projectName)
	} else {
		_, _ = fmt.Fprintf(stderr, "Using repository name: %s\n", projectName)
	}

	if err := sink.Set(ctx, "version", in.Version); err != nil {
		return err
	}

	if err := sink.Set(ctx, "version-no-v", strings.TrimPrefix(in.Version, "v")); err != nil {
		return err
	}

	return sink.Set(ctx, "project-name", projectName)
}
