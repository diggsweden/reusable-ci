// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func imageGroup() *cli.Command {
	return &cli.Command{
		Name:  flagImage,
		Usage: "single-image registry operations",
		Commands: []*cli.Command{
			imagePushCmd(),
			imageUsableDigestCmd(),
		},
	}
}

func imagePushCmd() *cli.Command {
	return &cli.Command{
		Name:  "push",
		Usage: "push a local Buildah image to a registry ref and print the verified digest",
		Description: `Pushes one local Buildah image to --destination, verifies the
raw manifest digest now served by the registry, emits digest/ref outputs, and
prints the verified digest to stdout for shell command substitution. The registry
digest is the source of truth; a valid Buildah digestfile must match it.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "local-image", Required: true, Sources: cli.EnvVars("LOCAL_IMAGE"), Usage: "local Buildah image name/ref to push"},
			&cli.StringFlag{Name: "destination", Required: true, Sources: cli.EnvVars("DESTINATION_REF", "IMAGE_REF"), Usage: "registry image ref to push, e.g. registry.example/owner/app:staging-amd64"},
			&cli.StringFlag{Name: flagAuthFile, Sources: cli.EnvVars("REUSABLE_CI_REGISTRY_AUTH_FILE"), Usage: "registry auth file for buildah push and registry digest verification"},
			&cli.StringFlag{Name: flagTLSVerify, Value: tlsVerifyDefault, Sources: cli.EnvVars("IMAGE_PUSH_TLS_VERIFY"), Usage: usageTLSVerify},
			&cli.IntFlag{Name: flagRetryAttempts, Value: 3, Sources: cli.EnvVars("IMAGE_PUSH_RETRY_ATTEMPTS"), Usage: "push and registry digest read attempts"},
			&cli.IntFlag{Name: flagRetryDelaySeconds, Value: 15, Sources: cli.EnvVars("IMAGE_PUSH_RETRY_DELAY_SECONDS"), Usage: "base delay between retry attempts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				registry := ociregistry.New()
				if authFile := cmd.String(flagAuthFile); authFile != "" {
					registry = ociregistry.WithAuthFile(authFile)
				}

				result, err := appcontainer.PushImage(ctx, buildah.New(), registry, dep.OutputSink, os.Stderr, appcontainer.PushImageInput{
					LocalImage:    cmd.String("local-image"),
					Destination:   cmd.String("destination"),
					AuthFile:      cmd.String(flagAuthFile),
					TLSVerify:     cmd.String(flagTLSVerify),
					RetryAttempts: cmd.Int(flagRetryAttempts),
					RetryDelay:    time.Duration(cmd.Int(flagRetryDelaySeconds)) * time.Second,
				})
				if err != nil {
					return err
				}

				_, _ = fmt.Fprintln(os.Stdout, result.Digest)

				return nil
			})
		},
	}
}

func imageUsableDigestCmd() *cli.Command {
	return &cli.Command{
		Name:  "usable-digest",
		Usage: "print an existing image digest only when architecture and labels match",
		Description: `Inspects --ref and prints its digest to stdout only when the
image has the required architecture and every --require-label key=value pair
matches the image config labels. Intended for safe tag reuse in workflows: a
non-matching or missing image exits non-zero and prints no digest.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_USABLE_DIGEST_REF"), Usage: "registry image ref to inspect, e.g. registry.example/owner/app:staging-amd64"},
			&cli.StringFlag{Name: flagArch, Required: true, Sources: cli.EnvVars("IMAGE_USABLE_DIGEST_ARCH"), Usage: "required image architecture, e.g. amd64 or arm64"},
			&cli.StringSliceFlag{Name: "require-label", Sources: cli.EnvVars("IMAGE_USABLE_DIGEST_REQUIRE_LABELS"), Usage: "required image config label as key=value (repeatable)"},
			&cli.StringFlag{Name: flagAuthFile, Sources: cli.EnvVars("REUSABLE_CI_REGISTRY_AUTH_FILE"), Usage: "registry auth file for digest/metadata inspection"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				registry := ociregistry.New()
				if authFile := cmd.String(flagAuthFile); authFile != "" {
					registry = ociregistry.WithAuthFile(authFile)
				}

				result, err := appcontainer.UsableImageDigest(ctx, registry, dep.OutputSink, appcontainer.UsableImageDigestInput{
					Ref:            cmd.String(flagRef),
					Arch:           cmd.String(flagArch),
					RequiredLabels: cmd.StringSlice("require-label"),
				})
				if err != nil {
					return err
				}

				_, _ = fmt.Fprintln(os.Stdout, result.Digest)

				return nil
			})
		},
	}
}
