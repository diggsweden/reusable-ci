// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package container wires `reusable-ci container ...` subcommands using
// urfave/cli v3. Subcommands live in this single file as long as they
// remain few; split when the file exceeds ~250 lines.
package container

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
)

// New returns the `container` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "container-image helpers (name resolution, namespace policy, tag/label metadata, …)",
		Commands: []*cli.Command{
			resolveNameCmd(),
			validateNamespaceCmd(),
			metadataCmd(),
			validateContainerfileCmd(),
			validateArtifactsCmd(),
			extractNPMTarballCmd(),
			suffixBinariesCmd(),
		},
	}
}

func resolveNameCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-name",
		Usage: "compute the canonical image reference and emit name=<value>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "registry",
				Required: true,
				Sources:  cli.EnvVars("CONTAINER_REGISTRY", "TARGET_REGISTRY"),
			},
			&cli.StringFlag{
				Name:    "image-name",
				Sources: cli.EnvVars("IMAGE_NAME_INPUT", "IMAGE_NAME"),
			},
			&cli.StringFlag{
				Name:     "repository",
				Required: true,
				Sources:  cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"),
			},
			&cli.StringFlag{
				Name:     "repository-owner",
				Required: true,
				Sources:  cli.EnvVars("REPOSITORY_OWNER", "GITHUB_REPOSITORY_OWNER"),
			},
			&cli.StringFlag{
				Name:    "name",
				Sources: cli.EnvVars("NAME"),
				Usage:   "optional sub-name for multi-container projects",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appcontainer.ResolveName(ctx, d.OutputSink, appcontainer.ResolveNameInput{
				Registry:        cmd.String("registry"),
				ImageName:       cmd.String("image-name"),
				Repository:      cmd.String("repository"),
				RepositoryOwner: cmd.String("repository-owner"),
				Name:            cmd.String("name"),
			})
		},
	}
}

func metadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "compute Docker tags + OCI labels from declarative tag rules",
		Description: "Functional replacement for the publish-container.yml call site of " +
			"docker/metadata-action. Reads TAG_RULES (newline csv lines), evaluates them " +
			"against the resolved provider event context, and writes tags/labels/version/json " +
			"to the platform output sink.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "image-name",
				Required: true,
				Sources:  cli.EnvVars("IMAGE_NAME"),
				Usage:    "single base image ref, e.g. ghcr.io/owner/repo",
			},
			&cli.StringFlag{
				Name:    "tag-rules",
				Sources: cli.EnvVars("TAG_RULES"),
				Usage:   "newline-separated csv tag-rule lines",
			},
			&cli.StringFlag{
				Name:    "flavor",
				Sources: cli.EnvVars("FLAVOR"),
				Usage:   "only `latest=false` is honoured; other entries are refused",
			},
			&cli.BoolFlag{
				Name:    "emit-labels",
				Sources: cli.EnvVars("EMIT_LABELS"),
				Usage:   "also emit org.opencontainers.image.* labels",
			},
			&cli.StringFlag{
				Name:    "oci-description",
				Sources: cli.EnvVars("OCI_DESCRIPTION"),
				Usage:   "override for org.opencontainers.image.description",
			},
			&cli.StringFlag{
				Name:    "oci-license",
				Sources: cli.EnvVars("OCI_LICENSE"),
				Usage:   "override for org.opencontainers.image.licenses (SPDX id)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appcontainer.ComputeMetadata(ctx, d.Provider, d.OutputSink, appcontainer.ComputeMetadataInput{
				ImageName:   cmd.String("image-name"),
				TagRules:    cmd.String("tag-rules"),
				Flavor:      cmd.String("flavor"),
				EmitLabels:  cmd.Bool("emit-labels"),
				Description: cmd.String("oci-description"),
				License:     cmd.String("oci-license"),
			})
			return err
		},
	}
}

func validateNamespaceCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-namespace",
		Usage: "verify an image lives in the allowed ghcr.io namespace (no-op on other registries)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "image-name",
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
				Sources:  cli.EnvVars("TARGET_REGISTRY", "CONTAINER_REGISTRY"),
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
			fmt.Printf("✓ Image namespace validated: %s\n", cmd.String("image-name"))
			return nil
		},
	}
}
