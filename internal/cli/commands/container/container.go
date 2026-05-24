// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package container wires `reusable-ci container ...` subcommands.
//
// Subcommand tree:
//
//   - validate.go — `validate <artifacts|containerfile|namespace>`
//   - manifest.go — `manifest <merge|inspect>`
//   - container.go (this file) — flat ops: resolve-name, platform-plan,
//     metadata, write-digest-marker
//   - tarball.go  — `extract-npm-tarball`
//   - binaries.go — `suffix-extracted-binaries`
//   - materialize_build_secrets.go — unpack the build-secrets envelope
//     into mode-0600 tmpfiles for BuildKit `--mount=type=secret` use
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

// New returns the `container` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "container-image helpers (name resolution, manifests, namespace policy, tag/label metadata, …)",
		Commands: []*cli.Command{
			validateGroup(),
			manifestGroup(),
			resolveNameCmd(),
			platformPlanCmd(),
			metadataCmd(),
			writeDigestMarkerCmd(),
			extractNPMTarballCmd(),
			suffixBinariesCmd(),
			signCmd(),
			materializeBuildSecretsCmd(),
		},
	}
}

func writeDigestMarkerCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-digest-marker",
		Usage: "write a validated digest marker file for manifest merging",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "digest", Sources: cli.EnvVars("DIGEST"), Usage: "sha256:… digest written into the per-arch marker file"},
			&cli.StringFlag{Name: "digests-dir", Value: domaincontainer.DefaultDigestsDir, Sources: cli.EnvVars("DIGESTS_DIR"), Usage: "directory where the marker file is written"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			path, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{
				Digest:     cmd.String("digest"),
				DigestsDir: cmd.String("digests-dir"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "digest marker: %s\n", path)

			return nil
		},
	}
}

func platformPlanCmd() *cli.Command {
	return &cli.Command{
		Name:  "platform-plan",
		Usage: "emit platform matrix JSON and per-platform suffix outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "platforms", Sources: cli.EnvVars("PLATFORMS"), Usage: "comma-separated build platforms (linux/amd64,linux/arm64)"},
			&cli.StringFlag{Name: "platform", Sources: cli.EnvVars("PLATFORM"), Usage: "single build platform; falls back to the first entry of --platforms"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appcontainer.PlatformPlan(ctx, d.OutputSink, os.Stderr, appcontainer.PlatformPlanInput{
					Platforms: cmd.String("platforms"),
					Platform:  cmd.String("platform"),
				})

				return err
			})
		},
	}
}

func resolveNameCmd() *cli.Command {
	return &cli.Command{
		Name:  "resolve-name",
		Usage: "compute the canonical image reference and emit name=<value>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "registry", Required: true, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry hostname (e.g. ghcr.io)"},
			&cli.StringFlag{Name: "image-name", Sources: cli.EnvVars("IMAGE_NAME"), Usage: "explicit image name override (defaults to <owner>/<repo>)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "repository", Required: true, Sources: cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"), Usage: "\"owner/repo\" used to build the default image name"},
			&cli.StringFlag{Name: "repository-owner", Required: true, Sources: cli.EnvVars("REPOSITORY_OWNER", "GITHUB_REPOSITORY_OWNER"), Usage: "owner portion used to lowercase-normalize the registry path"},
			&cli.StringFlag{Name: "name", Sources: cli.EnvVars("CONTAINER_NAME"),
				Usage: "optional sub-name for multi-container projects"},
			&cli.StringFlag{Name: "name-suffix", Sources: cli.EnvVars("IMAGE_NAME_SUFFIX"),
				Usage: "suffix appended to the repository segment of the derived image name " +
					"(e.g. `-dev`). Used by the dev-release flow to push to a separate namespace " +
					"so dev tags don't share a registry path with production releases. Ignored " +
					"when --image-name is set."},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appcontainer.ResolveName(ctx, d.OutputSink, appcontainer.ResolveNameInput{
					Registry:        cmd.String("registry"),
					ImageName:       cmd.String("image-name"),
					Repository:      cmd.String("repository"),
					RepositoryOwner: cmd.String("repository-owner"),
					Name:            cmd.String("name"),
					NameSuffix:      cmd.String("name-suffix"),
				})
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
			&cli.StringFlag{Name: "image-name", Required: true, Sources: cli.EnvVars("IMAGE_NAME"),
				Usage: "single base image ref, e.g. ghcr.io/owner/repo"},
			&cli.StringFlag{Name: "tag-rules", Sources: cli.EnvVars("TAG_RULES"),
				Usage: "newline-separated csv tag-rule lines"},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("FLAVOR"),
				Usage: "only `latest=false` is honoured; other entries are refused"},
			&cli.BoolFlag{Name: "emit-labels", Sources: cli.EnvVars("EMIT_LABELS"),
				Usage: "also emit org.opencontainers.image.* labels"},
			&cli.StringFlag{Name: "oci-description", Sources: cli.EnvVars("OCI_DESCRIPTION"),
				Usage: "override for org.opencontainers.image.description"},
			&cli.StringFlag{Name: "oci-license", Sources: cli.EnvVars("OCI_LICENSE"),
				Usage: "override for org.opencontainers.image.licenses (SPDX id)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appcontainer.ComputeMetadata(ctx, d.Provider, d.RepoMetadataFetcher(), d.OutputSink, appcontainer.ComputeMetadataInput{
					ImageName:   cmd.String("image-name"),
					TagRules:    cmd.String("tag-rules"),
					Flavor:      cmd.String("flavor"),
					EmitLabels:  cmd.Bool("emit-labels"),
					Description: cmd.String("oci-description"),
					License:     cmd.String("oci-license"),
				})

				return err
			})
		},
	}
}
