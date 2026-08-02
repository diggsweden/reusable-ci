// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
)

func signerImageGroup() *cli.Command {
	return &cli.Command{
		Name:  "signer-image",
		Usage: "build and assemble forgejo-ci signer images",
		Commands: []*cli.Command{
			signerImageBuildArchCmd(),
			signerImageAssembleCmd(),
		},
	}
}

func signerImageBuildArchCmd() *cli.Command {
	return &cli.Command{
		Name:  "build-arch",
		Usage: "build and push one forgejo-ci signer image architecture",
		Description: `Builds one signer image architecture, pushes its temporary
architecture tag, computes the registry-served manifest digest, and writes
signer-image-<arch>.json for the manifest assembly job.`,
		Flags: append(signerImageCommonFlags(),
			&cli.StringFlag{Name: flagArch, Sources: cli.EnvVars("SIGNER_ARCH"), Usage: "signer architecture: amd64 or arm64"},
			&cli.StringFlag{Name: flagContainerfile, Value: "packaging/signer/Containerfile", Sources: cli.EnvVars("SIGNER_CONTAINERFILE"), Usage: "signer image Containerfile"},
			&cli.StringFlag{Name: flagContext, Value: ".", Sources: cli.EnvVars("SIGNER_CONTEXT"), Usage: "signer image build context"},
			&cli.StringFlag{Name: "metadata-dir", Sources: cli.EnvVars("SIGNER_METADATA_DIR"), Usage: "directory for signer-image-<arch>.json (default signer-image-arch-<arch>)"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, err := appcontainer.BuildSignerImageArch(ctx, buildah.New(), os.Stderr, appcontainer.SignerImageBuildArchInput{
				AuthFile:      cmd.String(flagAuthFile),
				Arch:          cmd.String(flagArch),
				SourceSHA:     cmd.String(flagSourceSHA),
				ServerURL:     cmd.String(flagServerURL),
				Repository:    cmd.String(flagRepository),
				Containerfile: cmd.String(flagContainerfile),
				Context:       cmd.String(flagContext),
				MetadataDir:   cmd.String("metadata-dir"),
				RetryAttempts: cmd.Int(flagRetryAttempts),
				RetryDelay:    time.Duration(cmd.Int(flagRetryDelaySeconds)) * time.Second,
			})

			return err
		},
	}
}

func signerImageAssembleCmd() *cli.Command {
	return &cli.Command{
		Name:  "assemble",
		Usage: "assemble and push the forgejo-ci signer multi-arch manifest",
		Description: `Reads signer-image-arch-<arch>/signer-image-<arch>.json
metadata, validates that every source image is digest-pinned under the expected
repository, assembles the multi-arch manifest, and emits image-ref,
image-digest, and image-tag outputs.`,
		Flags: append(signerImageCommonFlags(),
			&cli.StringFlag{Name: "archs", Value: "amd64\narm64", Sources: cli.EnvVars("SIGNER_ARCHS"), Usage: "newline-separated signer architectures to include"},
			&cli.StringFlag{Name: "metadata-dir", Value: "signer-image-dist", Sources: cli.EnvVars("SIGNER_METADATA_DIR"), Usage: "directory for signer-image.json"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				_, err := appcontainer.AssembleSignerImageManifest(ctx, buildah.New(), dep.OutputSink, dep.SummarySink, os.Stderr, appcontainer.SignerImageAssembleInput{
					AuthFile:      cmd.String(flagAuthFile),
					SourceSHA:     cmd.String(flagSourceSHA),
					ServerURL:     cmd.String(flagServerURL),
					Repository:    cmd.String(flagRepository),
					Archs:         splitSignerLines(cmd.String("archs")),
					MetadataDir:   cmd.String("metadata-dir"),
					RetryAttempts: cmd.Int(flagRetryAttempts),
					RetryDelay:    time.Duration(cmd.Int(flagRetryDelaySeconds)) * time.Second,
				})

				return err
			})
		},
	}
}

func signerImageCommonFlags() []cli.Flag {
	return []cli.Flag{
		regflags.AuthFile(regflags.AuthFileOpts{Env: "REUSABLE_CI_SIGNER_AUTH_FILE", Usage: "registry auth file used by buildah/skopeo"}),
		&cli.StringFlag{Name: flagSourceSHA, Sources: cli.EnvVars("SOURCE_SHA"), Usage: "git commit SHA used in signer image tags and OCI revision label"},
		&cli.StringFlag{Name: flagServerURL, Sources: cienv.ServerURL(), Usage: "forge server URL used to derive the registry/repository"},
		&cli.StringFlag{Name: flagRepository, Sources: cienv.Repository(), Usage: "owner/repo used to derive the signer image repository"},
		&cli.IntFlag{Name: flagRetryAttempts, Value: 3, Sources: cli.EnvVars("SIGNER_IMAGE_RETRY_ATTEMPTS"), Usage: "registry push attempts"},
		&cli.IntFlag{Name: flagRetryDelaySeconds, Value: 10, Sources: cli.EnvVars("SIGNER_IMAGE_RETRY_DELAY_SECONDS"), Usage: "base delay between registry push retries"},
	}
}

func splitSignerLines(raw string) []string {
	var out []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}

	return out
}
