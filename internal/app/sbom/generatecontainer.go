// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// GenerateContainerInput drives GenerateContainer.
type GenerateContainerInput struct {
	// ArtifactTypes is the comma-separated list (e.g. "maven,npm").
	// Empty → a single generation pass with no artifact-type loop.
	ArtifactTypes string
	// RefName is the git ref (typically $CI_REF_NAME). A leading "v"
	// is stripped — `${CI_REF_NAME#v}`.
	RefName string
	// Repo is the org/repo string (typically $CI_REPO). The trailing
	// path component is used as the project name.
	Repo string
	// ImageName is the registry image name (e.g. ghcr.io/org/app).
	ImageName string
	// ImageDigest is the @sha256:... suffix the bash joins with @.
	ImageDigest string
}

// GenerateContainer is the thin wrapper around the SBOM generator for
// `analyzed-container` layer SBOMs produced after a multi-artifact
// container is published.
//
// When ArtifactTypes contains multiple comma-separated entries the
// inner generator runs once per entry, but every invocation writes to
// the same output filename — the analyzed-container layer is
// artefact-type-agnostic, so the loop is functionally a single write.
// The last entry's run wins; in practice every entry produces
// identical output, so the loop is observationally idempotent.
func GenerateContainer(
	ctx context.Context,
	syft SyftOps,
	mvn MavenOps,
	gitRepo GitOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in GenerateContainerInput,
) error {
	if in.RefName == "" {
		return fmt.Errorf("ref name is required: pass --ref-name <tag> or set $CI_REF_NAME (or $GITHUB_REF_NAME): %w", errs.ErrUsage)
	}

	if in.Repo == "" {
		return fmt.Errorf("repository is required: pass --repository <owner/repo> or set $CI_REPO (or $GITHUB_REPOSITORY): %w", errs.ErrUsage)
	}

	if in.ImageName == "" {
		return fmt.Errorf("image name is required: pass --image-name <ref> or set $IMAGE_NAME: %w", errs.ErrUsage)
	}

	if in.ImageDigest == "" {
		return fmt.Errorf("image digest is required: pass --image-digest <sha256:…> or set $IMAGE_DIGEST: %w", errs.ErrUsage)
	}

	version := strings.TrimPrefix(in.RefName, "v")
	projectName := filepath.Base(in.Repo)
	image := in.ImageName + "@" + in.ImageDigest

	gen := GenerateInput{
		Layers:         "analyzed-container",
		Version:        version,
		Name:           projectName,
		ContainerImage: image,
	}

	if in.ArtifactTypes == "" {
		_, _ = fmt.Fprintln(w, "No artifact dependencies - generating SBOM from container image only")

		if err := Generate(ctx, syft, mvn, gitRepo, w, stderr, gen); err != nil {
			return err
		}

		_, _ = fmt.Fprintln(w, "✓ Container SBOM generation completed")

		return nil
	}

	for _, t := range strings.Split(in.ArtifactTypes, ",") { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		_, _ = fmt.Fprintf(w, "Generating SBOM for artifact type: %s\n", t)

		if err := Generate(ctx, syft, mvn, gitRepo, w, stderr, gen); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(w, "✓ Container SBOM generation completed")

	return nil
}
