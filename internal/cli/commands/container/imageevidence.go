// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/skopeo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/trivy"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
)

func imageEvidenceCmd() *cli.Command {
	return &cli.Command{
		Name:  "image-evidence",
		Usage: "scan local OCI image evidence, including per-platform multi-arch layouts",
		Description: `Runs Trivy against a local OCI layout, never re-pulling the
image from the registry. With --local-image-ref, the image is first exported
from Buildah local storage to a temporary OCI layout, then scanned. When
--sbom-output is set, Syft scans the same local layout and writes CycloneDX JSON.
With --registry-digest-ref and one --platform, the selected registry image is
first copied to a temporary OCI layout, then scanned locally.

For multi-platform images, set repeated --platform values and
--trivy-output-template. reusable-ci prefers --scan-layout when set, otherwise
exports --local-manifest, otherwise pulls --registry-digest-ref or
--registry-ref@--digest. Each platform is split into a single-platform OCI layout
before Trivy runs, because Trivy's
--input mode does not select a platform from a manifest list.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "oci-layout", Sources: cli.EnvVars("IMAGE_EVIDENCE_OCI_LAYOUT"), Usage: "existing OCI layout directory to scan"},
			&cli.StringFlag{Name: "local-image-ref", Sources: cli.EnvVars("IMAGE_EVIDENCE_LOCAL_IMAGE_REF"), Usage: "Buildah local image reference to export to an OCI layout before scanning"},
			&cli.StringFlag{Name: "trivy-output", Sources: cli.EnvVars("IMAGE_EVIDENCE_TRIVY_OUTPUT"), Usage: "destination path for Trivy JSON output"},
			&cli.StringFlag{Name: "sbom-output", Sources: cli.EnvVars("IMAGE_EVIDENCE_SBOM_OUTPUT"), Usage: "optional destination path for CycloneDX SBOM output"},
			&cli.StringFlag{Name: "scan-layout", Sources: cli.EnvVars("IMAGE_EVIDENCE_SCAN_LAYOUT", "SCAN_LAYOUT"), Usage: "multi-platform OCI layout directory already tagged scan; preferred when set"},
			&cli.StringFlag{Name: "local-manifest", Sources: cli.EnvVars("IMAGE_EVIDENCE_LOCAL_MANIFEST", "LOCAL_MANIFEST"), Usage: "local Buildah manifest list to export before per-platform scans"},
			&cli.StringFlag{Name: "registry-digest-ref", Sources: cli.EnvVars("IMAGE_EVIDENCE_REGISTRY_DIGEST_REF"), Usage: "digest-pinned registry image ref, e.g. registry.example/owner/app@sha256:..."},
			&cli.StringFlag{Name: "registry-ref", Sources: cli.EnvVars("IMAGE_EVIDENCE_REGISTRY_REF", "IMAGE_REF"), Usage: "registry image ref used with --digest when no local source is available"},
			&cli.StringFlag{Name: flagDigest, Sources: cli.EnvVars("IMAGE_EVIDENCE_DIGEST", "DIGEST"), Usage: "registry image digest used with --registry-ref when no local source is available"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "registry auth file for digest-ref fallback sources"}),
			&cli.StringSliceFlag{Name: flagPlatform, Sources: cli.EnvVars("IMAGE_EVIDENCE_PLATFORMS", "PLATFORMS"), Usage: "platform to scan, e.g. linux/amd64 (repeatable or comma/space-separated via env)"},
			&cli.StringFlag{Name: "trivy-output-template", Sources: cli.EnvVars("IMAGE_EVIDENCE_TRIVY_OUTPUT_TEMPLATE"), Usage: "destination template for per-platform Trivy JSON; must contain {arch} or {platform}"},
			&cli.StringFlag{Name: "sbom-output-template", Sources: cli.EnvVars("IMAGE_EVIDENCE_SBOM_OUTPUT_TEMPLATE"), Usage: "optional per-platform CycloneDX SBOM template; must contain {arch} or {platform}"},
			&cli.StringFlag{Name: "trivy-timeout", Value: "30m", Sources: cli.EnvVars("IMAGE_EVIDENCE_TRIVY_TIMEOUT"), Usage: "Trivy scan timeout"},
			&cli.StringFlag{Name: "temp-dir", Sources: cienv.TempDir(), Usage: "scratch directory for the exported OCI layouts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			skopeoAdapter := skopeo.New()
			if authFile := cmd.String(flagAuthFile); authFile != "" {
				skopeoAdapter = skopeo.WithAuthFile(authFile)
			}

			return appcontainer.ImageEvidence(ctx,
				buildah.New(),
				skopeoAdapter,
				trivy.New(),
				&syft.Adapter{UnsetEnv: signerSecretEnv()},
				os.Stderr,
				os.Stderr,
				appcontainer.ImageEvidenceInput{
					OCILayout:           cmd.String("oci-layout"),
					LocalImageRef:       cmd.String("local-image-ref"),
					TrivyOutput:         cmd.String("trivy-output"),
					SBOMOutput:          cmd.String("sbom-output"),
					ScanLayout:          cmd.String("scan-layout"),
					LocalManifest:       cmd.String("local-manifest"),
					RegistryDigestRef:   cmd.String("registry-digest-ref"),
					RegistryRef:         cmd.String("registry-ref"),
					Digest:              cmd.String(flagDigest),
					Platforms:           cmd.StringSlice(flagPlatform),
					TrivyOutputTemplate: cmd.String("trivy-output-template"),
					SBOMOutputTemplate:  cmd.String("sbom-output-template"),
					TrivyTimeout:        cmd.String("trivy-timeout"),
					TempDir:             cmd.String("temp-dir"),
				},
			)
		},
	}
}
