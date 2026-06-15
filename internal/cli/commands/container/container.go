// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
	"github.com/diggsweden/reusable-ci/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/cli/secret"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// flagRegistry is the shared flag name for the registry host, named once so the
// container package stays under goconst's literal budget.
const flagRegistry = "registry"

// New returns the `container` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "container-image helpers (name resolution, manifests, namespace policy, tag/label metadata, …)",
		Commands: []*cli.Command{
			validateGroup(),
			manifestGroup(),
			ledgerGroup(),
			resolveNameCmd(),
			platformPlanCmd(),
			metadataCmd(),
			writeDigestMarkerCmd(),
			extractNPMTarballCmd(),
			suffixBinariesCmd(),
			signCmd(),
			materializeBuildSecretsCmd(),
			loginCmd(),
		},
	}
}

func loginCmd() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "write registry credentials to the shared OCI auth config (docker, podman, buildah, skopeo, cosign) — a node-less, forge-neutral replacement for docker/login-action",
		Description: `Writes {"auths":{...}} to $REGISTRY_AUTH_FILE / $DOCKER_CONFIG/config.json /
~/.docker/config.json (first that applies). The password is read from a file or
stdin or $REGISTRY_PASSWORD — never argv — and the file is written 0600.

EXAMPLES:
   echo "$TOKEN" | reusable-ci container login --registry ghcr.io --username "$GITHUB_ACTOR" --password-file -
   reusable-ci container login --registry codeberg.org --username bot   # password from $REGISTRY_PASSWORD`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRegistry, Value: domaincontainer.DefaultRegistry, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry host (e.g. ghcr.io, codeberg.org)"},
			&cli.StringFlag{Name: "username", Sources: cli.EnvVars("REGISTRY_USERNAME"), Usage: "registry username"},
			&cli.StringFlag{Name: "password-file", Usage: "file containing the password (\"-\" reads stdin); defaults to $REGISTRY_PASSWORD. The password never appears in argv."},
			&cli.StringFlag{Name: "auth-file", Usage: "override the auth config path (default: $REGISTRY_AUTH_FILE, else $DOCKER_CONFIG/config.json, else ~/.docker/config.json)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			password, err := secret.Resolve(cmd.String("password-file"), "REGISTRY_PASSWORD")
			if err != nil {
				return err
			}

			if password == "" {
				return fmt.Errorf("password is required: pipe it to --password-file - or set $REGISTRY_PASSWORD: %w", errs.ErrMissingInput)
			}

			return appcontainer.RegistryLogin(os.Stderr, appcontainer.RegistryLoginInput{
				Registry: cmd.String(flagRegistry),
				Username: cmd.String("username"),
				Password: password,
				AuthFile: cmd.String("auth-file"),
			})
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
			&cli.StringFlag{Name: "repository", Required: true, Sources: cienv.Repository(), Usage: "\"owner/repo\" used to build the default image name"},
			&cli.StringFlag{Name: "repository-owner", Required: true, Sources: cienv.RepositoryOwner(), Usage: "owner segment used to prefix bare image names on docker.io (Docker Hub); the derived reference is always lowercased for OCI compliance"},
			&cli.StringFlag{Name: "name", Sources: cli.EnvVars("CONTAINER_NAME"),
				Usage: "optional sub-name for multi-container projects"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appcontainer.ResolveName(ctx, d.OutputSink, appcontainer.ResolveNameInput{
					Registry:        cmd.String("registry"),
					ImageName:       cmd.String("image-name"),
					Repository:      cmd.String("repository"),
					RepositoryOwner: cmd.String("repository-owner"),
					Name:            cmd.String("name"),
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
				Usage: "only latest=false is honoured; other entries are refused"},
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
