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
// container is published. Mirrors
// scripts/sbom/generate-container-sbom-artifacts.sh +
// scripts/sbom/generate-container-sbom.sh end-to-end.
//
// Faithful note: when ArtifactTypes contains multiple comma-separated
// entries, the inner SBOM generator is invoked once per entry. Each
// invocation produces the same filename (the loop is vestigial in the
// bash; the layer doesn't consult artifact type) — the last write
// wins. Preserved for byte-compat with the bash.
func GenerateContainer(
	ctx context.Context,
	syft SyftOps,
	mvn MavenOps,
	gitRepo GitOps,
	stdout, stderr io.Writer,
	in GenerateContainerInput,
) error {
	if in.RefName == "" {
		return fmt.Errorf("CI_REF_NAME is required: %w", errs.ErrUsage)
	}
	if in.Repo == "" {
		return fmt.Errorf("CI_REPO is required: %w", errs.ErrUsage)
	}
	if in.ImageName == "" {
		return fmt.Errorf("IMAGE_NAME is required: %w", errs.ErrUsage)
	}
	if in.ImageDigest == "" {
		return fmt.Errorf("IMAGE_DIGEST is required: %w", errs.ErrUsage)
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
		fmt.Fprintln(stdout, "No artifact dependencies - generating SBOM from container image only")
		if err := Generate(ctx, syft, mvn, gitRepo, stdout, stderr, gen); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "✓ Container SBOM generation completed")
		return nil
	}
	for _, t := range strings.Split(in.ArtifactTypes, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		fmt.Fprintf(stdout, "Generating SBOM for artifact type: %s\n", t)
		if err := Generate(ctx, syft, mvn, gitRepo, stdout, stderr, gen); err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "✓ Container SBOM generation completed")
	return nil
}
