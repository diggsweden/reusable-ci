// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// validateGroup wires `reusable-ci container validate <verb>` —
// per-aspect container validation (artifact presence, Containerfile
// path resolution, registry-namespace policy).
func validateGroup() *cli.Command {
	return &cli.Command{
		Name:  subCmdValidate,
		Usage: "validate one aspect of the container build inputs",
		Commands: []*cli.Command{
			validateArtifactsCmd(),
			validateContainerfileCmd(),
			validateNamespaceCmd(),
		},
	}
}

func validateArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifacts",
		Usage: "verify project-type-specific artifacts are present, warn on COPY-instead-of-rebuild policy",
		Description: `EXAMPLE:
   reusable-ci container validate artifacts --project-type maven --artifact-dir target --containerfile Containerfile`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project-type",
				Required: true,
				Sources:  cli.EnvVars("PROJECT_TYPE"),
				Usage:    "ecosystem of the produced artifacts (maven/gradle/npm/go/cargo)",
			},
			&cli.StringFlag{
				Name:     "artifact-dir",
				Required: true,
				Sources:  cli.EnvVars("ARTIFACT_DIR"),
				Usage:    "directory holding the built artifacts to inspect",
			},
			&cli.StringFlag{
				Name:    flagContainerfile,
				Sources: cli.EnvVars("CONTAINERFILE"),
				Usage:   "Containerfile path (enables COPY-vs-rebuild policy checks)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appcontainer.ValidateArtifacts(os.Stderr, os.Stderr, annot, appcontainer.ValidateArtifactsInput{
				ProjectType:       cmd.String("project-type"),
				ArtifactDir:       cmd.String("artifact-dir"),
				ContainerfilePath: cmd.String(flagContainerfile),
			})
		},
	}
}

func validateContainerfileCmd() *cli.Command {
	return &cli.Command{
		Name:  flagContainerfile,
		Usage: "verify a Containerfile path exists (or glob-resolves uniquely), emit containerfile=<path>",
		Description: `EXAMPLE:
   reusable-ci container validate containerfile --path Containerfile`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "path",
				Required: true,
				Sources:  cli.EnvVars("CONTAINERFILE"),
				Usage:    "Containerfile path or glob (e.g. Containerfile, src/*Containerfile)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appcontainer.ValidateContainerfile(ctx, d.OutputSink, os.Stderr, appcontainer.ValidateContainerfileInput{
					Path: cmd.String("path"),
				})
			})
		},
	}
}

func validateNamespaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "namespace",
		Usage: "verify an image lives in the allowed namespace for an enforced registry (no-op for registries not in --enforce-namespace-on)",
		Description: `EXAMPLE:
   reusable-ci container validate namespace --image-name ghcr.io/org/app \
     --repository org/app --registry ghcr.io --enforce-namespace org`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagImageName,
				Required: true,
				Sources:  cli.EnvVars("IMAGE_NAME"),
				Usage:    "image ref (registry/owner/name) whose namespace is checked",
			},
			&cli.StringFlag{
				Name:     flagRepository,
				Required: true,
				Sources:  cienv.Repository(),
				Usage:    "\"owner/repo\" the image must be namespaced under on an enforced registry",
			},
			&cli.StringFlag{
				Name:     "registry",
				Required: true,
				Sources:  cli.EnvVars("CONTAINER_REGISTRY"),
				Usage:    "registry hostname the image is pushed to (checked against --enforce-namespace-on)",
			},
			&cli.StringFlag{
				Name:     "enforce-namespace",
				Required: true,
				Sources:  cli.EnvVars("ENFORCE_NAMESPACE"),
				Usage:    "required namespace prefix (e.g. the owner) the image name must start with",
			},
			&cli.StringSliceFlag{
				Name:    "enforce-namespace-on",
				Value:   []string{domaincontainer.DefaultRegistry},
				Sources: cli.EnvVars("ENFORCE_NAMESPACE_ON"),
				Usage:   "registries whose namespace policy this deployment enforces (default ghcr.io); set to your registry when self-hosting so the check runs instead of silently passing",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			err := appcontainer.ValidateNamespace(domaincontainer.ValidateNamespaceInput{
				ImageName:           cmd.String(flagImageName),
				Repository:          cmd.String(flagRepository),
				Registry:            cmd.String("registry"),
				EnforceNamespace:    cmd.String("enforce-namespace"),
				EnforceOnRegistries: cmd.StringSlice("enforce-namespace-on"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "✓ Image namespace validated: %s\n", cmd.String(flagImageName))

			return nil
		},
	}
}
