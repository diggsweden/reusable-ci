// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func buildPushOCIImageCmd() *cli.Command {
	return &cli.Command{
		Name:  "build-push-oci-image",
		Usage: "build and push a multi-arch OCI image manifest list with buildah",
		Description: `Builds every platform from a JSON plan into one buildah
manifest, applies the standard OCI release labels, pushes the manifest list, and
emits image and digest outputs. This is the reusable-ci implementation of the
forgejo-ci public build-push-oci-image action contract.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagTag, Sources: cli.EnvVars("IMAGE_TAG"), Usage: "image tag to build and push"},
			&cli.StringFlag{Name: flagContainerfile, Sources: cli.EnvVars("CONTAINER_FILE"), Usage: "path to the Containerfile/Dockerfile"},
			&cli.StringFlag{Name: "builds-json", Sources: cli.EnvVars("BUILDS_JSON"), Usage: `JSON array of per-platform builds: [{"platform":"linux/amd64","build-args":["KEY=VALUE"]}]`},
			&cli.StringFlag{Name: flagImage, Sources: cli.EnvVars("IMAGE_NAME"), Usage: "image base (registry/owner/name); defaults to server host + lowercased repository"},
			&cli.StringFlag{Name: flagContext, Value: ".", Sources: cli.EnvVars("BUILD_CONTEXT"), Usage: "build context directory"},
			&cli.StringFlag{Name: "title", Sources: cli.EnvVars("OCI_LABEL_TITLE"), Usage: "org.opencontainers.image.title"},
			&cli.StringFlag{Name: "description", Sources: cli.EnvVars("OCI_LABEL_DESCRIPTION"), Usage: "org.opencontainers.image.description"},
			&cli.StringFlag{Name: "licenses", Sources: cli.EnvVars("OCI_LABEL_LICENSES"), Usage: "org.opencontainers.image.licenses"},
			&cli.StringFlag{Name: "vendor", Sources: cli.EnvVars("OCI_LABEL_VENDOR"), Usage: "org.opencontainers.image.vendor"},
			&cli.StringFlag{Name: "authors", Sources: cli.EnvVars("OCI_LABEL_AUTHORS"), Usage: "org.opencontainers.image.authors"},
			&cli.StringFlag{Name: "documentation", Sources: cli.EnvVars("OCI_LABEL_DOCUMENTATION"), Usage: "org.opencontainers.image.documentation; defaults to <source>#readme"},
			&cli.StringFlag{Name: flagRefName, Sources: cli.EnvVars("OCI_LABEL_REF_NAME"), Usage: "org.opencontainers.image.ref.name; defaults to --tag"},
			&cli.StringFlag{Name: flagVersion, Sources: cli.EnvVars("OCI_LABEL_VERSION"), Usage: "org.opencontainers.image.version; defaults to --tag"},
			&cli.StringFlag{Name: flagRevision, Sources: cli.EnvVars("OCI_LABEL_REVISION"), Usage: "org.opencontainers.image.revision; defaults to git rev-parse HEAD"},
			&cli.StringFlag{Name: flagAuthFile, Sources: cli.EnvVars("REUSABLE_CI_REGISTRY_AUTH_FILE"), Usage: "registry auth file for buildah pulls and manifest push"},
			&cli.StringFlag{Name: flagTLSVerify, Value: tlsVerifyDefault, Sources: cli.EnvVars("BUILD_PUSH_TLS_VERIFY"), Usage: usageTLSVerify},
			&cli.StringFlag{Name: flagServerURL, Sources: cienv.ServerURL(), Usage: "forge server URL used to derive defaults and source labels"},
			&cli.StringFlag{Name: flagRepository, Sources: cienv.Repository(), Usage: "owner/repo used to derive defaults and source labels"},
			&cli.IntFlag{Name: flagRetryAttempts, Value: 3, Sources: cli.EnvVars("MANIFEST_PUSH_RETRY_ATTEMPTS"), Usage: "manifest push attempts"},
			&cli.IntFlag{Name: flagRetryDelaySeconds, Value: 15, Sources: cli.EnvVars("MANIFEST_PUSH_RETRY_DELAY_SECONDS"), Usage: "base delay between manifest push retry attempts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				_, err := appcontainer.BuildPushOCIImage(ctx, buildah.New(), git.New(), dep.OutputSink, os.Stderr, appcontainer.BuildPushOCIImageInput{
					Tag:           cmd.String(flagTag),
					Containerfile: cmd.String(flagContainerfile),
					BuildsJSON:    cmd.String("builds-json"),
					Image:         cmd.String(flagImage),
					Context:       cmd.String(flagContext),
					Title:         cmd.String("title"),
					Description:   cmd.String("description"),
					Licenses:      cmd.String("licenses"),
					Vendor:        cmd.String("vendor"),
					Authors:       cmd.String("authors"),
					Documentation: cmd.String("documentation"),
					RefName:       cmd.String(flagRefName),
					Version:       cmd.String(flagVersion),
					Revision:      cmd.String(flagRevision),
					AuthFile:      cmd.String(flagAuthFile),
					TLSVerify:     cmd.String(flagTLSVerify),
					ServerURL:     cmd.String(flagServerURL),
					Repository:    cmd.String(flagRepository),
					RetryAttempts: cmd.Int(flagRetryAttempts),
					RetryDelay:    time.Duration(cmd.Int(flagRetryDelaySeconds)) * time.Second,
				})

				return err
			})
		},
	}
}
