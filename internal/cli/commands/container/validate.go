// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
)

// validateGroup wires `reusable-ci container validate <verb>` —
// per-aspect container validation (artifact presence, Containerfile
// path resolution, registry-namespace policy).
func validateGroup() *cli.Command {
	return &cli.Command{
		Name:  "validate",
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
				Name:    "containerfile",
				Sources: cli.EnvVars("CONTAINERFILE"),
				Usage:   "Containerfile path (enables COPY-vs-rebuild policy checks)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appcontainer.ValidateArtifacts(os.Stderr, os.Stderr, annot, appcontainer.ValidateArtifactsInput{
				ProjectType:       cmd.String("project-type"),
				ArtifactDir:       cmd.String("artifact-dir"),
				ContainerfilePath: cmd.String("containerfile"),
			})
		},
	}
}

func validateContainerfileCmd() *cli.Command {
	return &cli.Command{
		Name:  "containerfile",
		Usage: "verify a Containerfile path exists (or glob-resolves uniquely), emit containerfile=<path>",
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
		Usage: "verify an image lives in the allowed ghcr.io namespace (no-op on other registries)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "image-name", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Required: true,
				Sources:  cli.EnvVars("IMAGE_NAME"),
			},
			&cli.StringFlag{
				Name:     "repository",
				Required: true,
				Sources:  cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"),
			},
			&cli.StringFlag{
				Name:     "registry",
				Required: true,
				Sources:  cli.EnvVars("CONTAINER_REGISTRY"),
			},
			&cli.StringFlag{
				Name:     "enforce-namespace",
				Required: true,
				Sources:  cli.EnvVars("ENFORCE_NAMESPACE"),
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			err := appcontainer.ValidateNamespace(domaincontainer.ValidateNamespaceInput{
				ImageName:        cmd.String("image-name"),
				Repository:       cmd.String("repository"),
				Registry:         cmd.String("registry"),
				EnforceNamespace: cmd.String("enforce-namespace"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "✓ Image namespace validated: %s\n", cmd.String("image-name"))

			return nil
		},
	}
}
