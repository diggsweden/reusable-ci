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
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// manifestGroup wires `reusable-ci container manifest <verb>` —
// container manifest-list operations: merge per-platform digests
// into a single manifest, or inspect an existing one.
func manifestGroup() *cli.Command {
	return &cli.Command{
		Name:  "manifest",
		Usage: "inspect, digest, merge, and push container manifests",
		Commands: []*cli.Command{
			manifestDigestCmd(),
			manifestMergeCmd(),
			manifestInspectCmd(),
			manifestPushCmd(),
		},
	}
}

func manifestDigestCmd() *cli.Command {
	return &cli.Command{
		Name:  "digest",
		Usage: "print the registry-served raw manifest digest and optionally verify a digestfile",
		Description: `Fetches the raw manifest for --ref, computes its sha256 digest,
optionally verifies a Buildah --digestfile value against it, emits digest/ref
outputs, and prints the verified digest to stdout. This works for single image
manifests and multi-platform indexes alike.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF", "MANIFEST_REF"), Usage: "registry image ref whose raw manifest digest should be computed"},
			&cli.StringFlag{Name: "digest-file", Sources: cli.EnvVars("MANIFEST_DIGEST_FILE"), Usage: "optional Buildah digestfile to verify against the registry digest"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "registry auth file for registry digest verification"}),
			&cli.IntFlag{Name: flagRetryAttempts, Value: 3, Sources: cli.EnvVars("MANIFEST_DIGEST_RETRY_ATTEMPTS"), Usage: "registry digest read attempts"},
			&cli.IntFlag{Name: flagRetryDelaySeconds, Value: 15, Sources: cli.EnvVars("MANIFEST_DIGEST_RETRY_DELAY_SECONDS"), Usage: "base delay between registry digest read attempts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				registry := ociregistry.New()
				if authFile := cmd.String(flagAuthFile); authFile != "" {
					registry = ociregistry.WithAuthFile(authFile)
				}

				result, err := appcontainer.ResolvePushedManifestDigest(ctx, registry, dep.OutputSink, os.Stderr, appcontainer.PushedManifestDigestInput{
					Ref:           cmd.String(flagRef),
					DigestFile:    cmd.String("digest-file"),
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

func manifestPushCmd() *cli.Command {
	return &cli.Command{
		Name:  "push",
		Usage: "push a local Buildah manifest list to a registry ref and print the verified digest",
		Description: `Pushes a local Buildah manifest list to --destination, verifies
the digest now served by the registry, emits digest/ref outputs, and prints the
verified digest to stdout for shell command substitution. The registry digest is
the source of truth; a valid Buildah digestfile must match it.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "local-manifest", Required: true, Sources: cli.EnvVars("LOCAL_MANIFEST"), Usage: "local Buildah manifest list name to push"},
			&cli.StringFlag{Name: "destination", Required: true, Sources: cli.EnvVars("DESTINATION_REF", "IMAGE_REF"), Usage: "registry image ref to push, e.g. registry.example/owner/app:staging-v1"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "registry auth file for buildah push and registry digest verification"}),
			regflags.TLSVerify(regflags.TLSVerifyOpts{Env: "MANIFEST_PUSH_TLS_VERIFY"}),
			&cli.BoolFlag{Name: "remove-local", Sources: cli.EnvVars("MANIFEST_PUSH_REMOVE_LOCAL"), Usage: "pass --rm to buildah manifest push after a successful registry push"},
			&cli.IntFlag{Name: flagRetryAttempts, Value: 3, Sources: cli.EnvVars("MANIFEST_PUSH_RETRY_ATTEMPTS"), Usage: "push and registry digest read attempts"},
			&cli.IntFlag{Name: flagRetryDelaySeconds, Value: 15, Sources: cli.EnvVars("MANIFEST_PUSH_RETRY_DELAY_SECONDS"), Usage: "base delay between retry attempts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				auth := regflags.Resolve(cmd)

				registry := ociregistry.New()
				if auth.AuthFile != "" {
					registry = ociregistry.WithAuthFile(auth.AuthFile)
				}

				result, err := appcontainer.PushManifest(ctx, buildah.New(), registry, dep.OutputSink, os.Stderr, appcontainer.PushManifestInput{
					LocalManifest: cmd.String("local-manifest"),
					Destination:   cmd.String("destination"),
					AuthFile:      auth.AuthFile,
					TLSVerify:     auth.TLSVerify,
					RemoveLocal:   cmd.Bool("remove-local"),
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

func manifestMergeCmd() *cli.Command {
	return &cli.Command{
		Name:  "merge",
		Usage: "create a manifest list from digest marker files and tags",
		Description: `EXAMPLE:
   # Assemble a multi-arch index from per-arch digest markers and apply tags
   reusable-ci container manifest merge --image-name ghcr.io/org/app \
     --tags "v1.2.3" --digests-dir /tmp/digests`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagImageName, Sources: cli.EnvVars("IMAGE_NAME"), Usage: "base image name (without tag) the manifest list points to"},
			&cli.StringFlag{Name: "tags", Sources: cli.EnvVars("TAGS"), Usage: "newline-separated tags to publish for the manifest list"},
			&cli.StringFlag{Name: "digests-dir", Value: domaincontainer.DefaultDigestsDir, Sources: cli.EnvVars("DIGESTS_DIR"), Usage: "directory holding the per-arch digest marker files"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appcontainer.MergeManifest(ctx, ociregistry.New(), os.Stderr, appcontainer.MergeManifestInput{
				ImageName:  cmd.String(flagImageName),
				Tags:       cmd.String("tags"),
				DigestsDir: cmd.String("digests-dir"),
			})
		},
	}
}

func manifestInspectCmd() *cli.Command {
	return &cli.Command{
		Name:  "inspect",
		Usage: "inspect a pushed manifest list and emit image/digest outputs",
		Description: `EXAMPLE:
   reusable-ci container manifest inspect --image-ref ghcr.io/org/app:v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "image-ref", Sources: cli.EnvVars("IMAGE_REF"), Usage: "fully-qualified image reference (registry/owner/name:tag) to inspect"},
			&cli.StringFlag{Name: "tags", Sources: cli.EnvVars("TAGS"), Usage: "newline-separated tags whose digest output is emitted to the sink"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appcontainer.InspectManifest(ctx, ociregistry.New(), d.OutputSink, os.Stderr, appcontainer.InspectManifestInput{
					Image: cmd.String("image-ref"),
					Tags:  cmd.String("tags"),
				})

				return err
			})
		},
	}
}
