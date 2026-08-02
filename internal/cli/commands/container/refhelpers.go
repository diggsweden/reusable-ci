// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
)

func containerfileArgDefaultCmd() *cli.Command {
	return &cli.Command{
		Name:  "containerfile-arg-default",
		Usage: "print an ARG default declared before the first FROM in a Containerfile",
		Description: `Reads a Containerfile/Dockerfile and prints the value from ARG NAME=...
before the first FROM. This is intentionally a narrow helper for base-image
defaults used by bespoke Buildah workflows, not a full Dockerfile frontend.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFile, Required: true, Sources: cli.EnvVars("CONTAINERFILE"), Usage: "Containerfile/Dockerfile path"},
			&cli.StringFlag{Name: flagName, Required: true, Sources: cli.EnvVars("ARG_NAME"), Usage: "ARG name to read"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			value, err := appcontainer.ContainerfileArgDefault(appcontainer.ContainerfileArgDefaultInput{
				File: cmd.String(flagFile),
				Name: cmd.String(flagName),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

// refGroup wires `reusable-ci container ref <verb>` — projections of one
// parsed image-reference concept. canonical/name are pure string math over an
// existing ref, platform additionally does registry I/O to pick a
// per-platform digest, and resolve composes a new canonical reference from CI
// repository context instead of parsing one.
func refGroup() *cli.Command {
	return &cli.Command{
		Name:  "ref",
		Usage: "image-reference helpers: canonicalize, strip to name, resolve per-platform digests, compose from repo context",
		Commands: []*cli.Command{
			refCanonicalCmd(),
			refNameCmd(),
			refPlatformCmd(),
			refResolveCmd(),
		},
	}
}

func refCanonicalCmd() *cli.Command {
	return &cli.Command{
		Name:  "canonical",
		Usage: "canonicalize a Docker image reference for digest-pinned Buildah use",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "image reference"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			value, err := appcontainer.CanonicalRef(cmd.String(flagRef))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

func refNameCmd() *cli.Command {
	return &cli.Command{
		Name:  "name",
		Usage: "print an image repository/name with any tag or digest removed",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "image reference"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			value, err := appcontainer.ImageNameForRef(cmd.String(flagRef))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

func refPlatformCmd() *cli.Command {
	return &cli.Command{
		Name:  "platform",
		Usage: "resolve an image index reference to one platform's digest-pinned ref",
		Description: `Fetches the raw manifest for --ref. Plain single-platform manifests
return the canonical ref unchanged; multi-platform indexes return image@digest for
the requested --platform. --auth-file accepts the Docker/containers auth config
written by ` + "`container login`" + `.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "image tag or digest ref to inspect"},
			&cli.StringFlag{Name: flagPlatform, Required: true, Sources: cli.EnvVars("PLATFORM"), Usage: "target platform: os/arch or os/arch/variant"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "Docker-compatible registry auth config"}),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			registry := ociregistry.New()
			if authFile := cmd.String(flagAuthFile); authFile != "" {
				registry = ociregistry.WithAuthFile(authFile)
			}

			value, err := appcontainer.PlatformRef(ctx, registry, appcontainer.PlatformRefInput{
				Ref:      cmd.String(flagRef),
				Platform: cmd.String(flagPlatform),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

func refResolveCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve",
		Usage: "compute the canonical image reference and emit name=<value>",
		Description: `EXAMPLES:
   # Canonical ghcr.io reference for a repo (emits name=ghcr.io/org/app)
   reusable-ci container ref resolve --registry ghcr.io --repository org/app --repository-owner org`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRegistry, Required: true, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry hostname (e.g. ghcr.io)"},
			&cli.StringFlag{Name: flagImageName, Sources: cli.EnvVars("IMAGE_NAME"), Usage: "explicit image name override (defaults to <owner>/<repo>)"},
			&cli.StringFlag{Name: flagRepository, Required: true, Sources: cienv.Repository(), Usage: "\"owner/repo\" used to build the default image name"},
			&cli.StringFlag{Name: "repository-owner", Required: true, Sources: cienv.RepositoryOwner(), Usage: "owner segment used to prefix bare image names on docker.io (Docker Hub); the derived reference is always lowercased for OCI compliance"},
			&cli.StringFlag{Name: flagName, Sources: cli.EnvVars("CONTAINER_NAME"),
				Usage: "optional sub-name for multi-container projects"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appcontainer.ResolveName(ctx, d.OutputSink, appcontainer.ResolveNameInput{
					Registry:        cmd.String(flagRegistry),
					ImageName:       cmd.String(flagImageName),
					Repository:      cmd.String(flagRepository),
					RepositoryOwner: cmd.String("repository-owner"),
					Name:            cmd.String(flagName),
				})
			})
		},
	}
}
