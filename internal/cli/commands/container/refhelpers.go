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
			&cli.StringFlag{Name: "name", Required: true, Sources: cli.EnvVars("ARG_NAME"), Usage: "ARG name to read"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			value, err := appcontainer.ContainerfileArgDefault(appcontainer.ContainerfileArgDefaultInput{
				File: cmd.String(flagFile),
				Name: cmd.String("name"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

func canonicalRefCmd() *cli.Command {
	return &cli.Command{
		Name:  "canonical-ref",
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

func imageNameForRefCmd() *cli.Command {
	return &cli.Command{
		Name:  "image-name-for-ref",
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

func platformRefCmd() *cli.Command {
	return &cli.Command{
		Name:  "platform-ref",
		Usage: "resolve an image index reference to one platform's digest-pinned ref",
		Description: `Fetches the raw manifest for --ref. Plain single-platform manifests
return the canonical ref unchanged; multi-platform indexes return image@digest for
the requested --platform. --auth-file accepts the Docker/containers auth config
written by ` + "`container login`" + `.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "image tag or digest ref to inspect"},
			&cli.StringFlag{Name: flagPlatform, Required: true, Sources: cli.EnvVars("PLATFORM"), Usage: "target platform: os/arch or os/arch/variant"},
			&cli.StringFlag{Name: flagAuthFile, Sources: cli.EnvVars("REUSABLE_CI_REGISTRY_AUTH_FILE"), Usage: "Docker-compatible registry auth config"},
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
