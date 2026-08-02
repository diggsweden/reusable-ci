// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/trivy"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// planScopeBuild is the plan-file scope of `container build`: the
// $REUSABLE_CI_PLAN JSON object under this key feeds the build flags
// (flag > plan > env > default).
const planScopeBuild = "container build"

func buildCmd() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "build a single native-platform container image with buildah, optionally pushing it by digest",
		Description: `Builds ONE native-platform image (the multi-arch index is assembled separately
by ` + "`container manifest merge`" + `) and, in push-by-digest mode, emits the pushed
digest for that merge. Authentication is the shared {"auths"} config written by
` + "`container login`" + `; secrets are read from the 0600 tmpfiles materialized by
` + "`container materialize-build-secrets`" + ` and mounted via ` + "`RUN --mount=type=secret`" + `;
the layer cache is a registry repo. Every flag may also be fed from the
$REUSABLE_CI_PLAN plan file under the "container build" scope
(flag > plan > env > default).

MODES (--mode):
   push-by-digest  build + push, emit the digest (no tag of its own) — publish-container
   load            build into local storage under --image-ref, for smoke tests — self-runtime
   local           export a stage's filesystem to --output-dir — binary extraction

EXAMPLES:
   # Build a single platform and push by digest (the publish-container path)
   reusable-ci container build --file Containerfile --platform linux/amd64 \
     --mode push-by-digest --image-ref ghcr.io/org/app

   # Build into local storage for a smoke test
   reusable-ci container build --mode load --image-ref app:test

   # Build, push by digest, and scan the pushed image in-process
   reusable-ci container build --mode push-by-digest --image-ref ghcr.io/org/app \
     --platform linux/amd64 --scan --scan-severity CRITICAL,HIGH`,
		Flags:  append(buildImageFlags(), scanFlags()...),
		Action: runBuild,
	}
}

// runBuild builds one platform image and, when --scan is set and a digest
// was pushed, scans it in-process.
func runBuild(ctx context.Context, cmd *cli.Command) error {
	return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
		_, err := appcontainer.BuildAndScan(ctx, buildah.New(), ociregistry.New(), trivy.New(), dep.OutputSink, os.Stderr, os.Stderr, deps.Annotator(cmd),
			appcontainer.BuildAndScanInput{
				Build:        buildImageInputFromCmd(cmd),
				EnableScan:   cmd.Bool("scan"),
				ScanSeverity: cmd.String("scan-severity"),
				TrivyVersion: cmd.String("trivy-version"),
			})

		return err
	})
}

// scanFlags returns the in-process scan flags of `container build`.
func scanFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "scan", Sources: planfile.Vars(planScopeBuild, "scan", "ENABLE_SCAN"), Usage: "scan the pushed image with trivy and fail on findings at/above --scan-severity (push-by-digest mode only)"},
		&cli.StringFlag{Name: "scan-severity", Value: "CRITICAL,HIGH", Sources: planfile.Vars(planScopeBuild, "scan-severity", "SCAN_SEVERITY"), Usage: "trivy severity filter AND fail-on threshold"},
		&cli.StringFlag{Name: "trivy-version", Sources: planfile.Vars(planScopeBuild, "trivy-version", "TRIVY_VERSION"), Usage: "version embedded in the GitLab container-scanning report"},
	}
}

// buildImageFlags returns the core build flags of `container build`.
// A fresh slice each call so callers can append safely.
func buildImageFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: flagContext, Value: ".", Sources: planfile.Vars(planScopeBuild, "context", "BUILD_CONTEXT"), Usage: "build context directory"},
		&cli.StringFlag{Name: flagFile, Sources: planfile.Vars(planScopeBuild, "file", "CONTAINER_FILE"), Usage: "path to the Containerfile/Dockerfile"},
		&cli.StringFlag{Name: "target", Sources: planfile.Vars(planScopeBuild, "target", "BUILD_TARGET"), Usage: "multi-stage target stage to build"},
		&cli.StringFlag{Name: flagPlatform, Sources: planfile.Vars(planScopeBuild, "platform", "BUILD_PLATFORM"), Usage: "single target platform, e.g. linux/arm64 (built natively)"},
		&cli.StringFlag{Name: "build-args", Sources: planfile.Vars(planScopeBuild, "build-args", "BUILD_ARGS"), Usage: "newline-separated KEY=VALUE build args"},
		&cli.StringFlag{Name: "secrets", Sources: planfile.Vars(planScopeBuild, "secrets", "BUILD_SECRETS"), Usage: "newline-separated id=NAME,src=PATH secrets (the secret-mounts output of `container materialize-build-secrets`)"},
		&cli.StringFlag{Name: "labels", Sources: planfile.Vars(planScopeBuild, "labels", "LABELS"), Usage: "newline-separated key=value OCI labels (the labels output of `container metadata`)"},
		&cli.StringFlag{Name: "source-date-epoch", Sources: planfile.Vars(planScopeBuild, "source-date-epoch", "SOURCE_DATE_EPOCH"), Usage: "unix seconds; clamps image/layer timestamps for reproducible digests"},
		&cli.StringFlag{Name: "cache-repo", Sources: planfile.Vars(planScopeBuild, "cache-repo", "BUILD_CACHE_REPO"), Usage: "dedicated registry repo for the layer cache (e.g. ghcr.io/org/buildcache). Empty disables caching. Imported best-effort; --cache-push also exports."},
		&cli.StringFlag{Name: "cache-scope", Sources: planfile.Vars(planScopeBuild, "cache-scope", "BUILD_CACHE_SCOPE"), Usage: "tag scope for the cache repo, e.g. <image>-<arch>; the ref is <cache-repo>:<cache-scope>"},
		&cli.BoolFlag{Name: "cache-push", Sources: planfile.Vars(planScopeBuild, "cache-push", "BUILD_CACHE_PUSH"), Usage: "also EXPORT the layer cache (not just import). Set only on a trusted push — never a fork PR."},
		&cli.StringFlag{Name: "mode", Required: true, Sources: planfile.Vars(planScopeBuild, "mode", "BUILD_MODE"), Usage: "output mode: push-by-digest | load | local"},
		&cli.StringFlag{Name: "image-ref", Sources: planfile.Vars(planScopeBuild, "image-ref", "IMAGE_REF"), Usage: "push target (push-by-digest) or local tag (load)"},
		&cli.StringFlag{Name: "output-dir", Sources: planfile.Vars(planScopeBuild, "output-dir", "OUTPUT_DIR"), Usage: "destination directory for local-export mode"},
		&cli.StringFlag{Name: "digest-key", Value: "digest", Sources: planfile.Vars(planScopeBuild, "digest-key", "DIGEST_KEY"), Usage: "OutputSink key for the pushed digest (push-by-digest mode)"},
	}
}

func buildImageInputFromCmd(cmd *cli.Command) appcontainer.BuildImageInput {
	return appcontainer.BuildImageInput{
		Context:         cmd.String(flagContext),
		Containerfile:   cmd.String(flagFile),
		Target:          cmd.String("target"),
		Platform:        cmd.String(flagPlatform),
		BuildArgs:       splitLines(cmd.String("build-args")),
		Secrets:         splitLines(cmd.String("secrets")),
		Labels:          splitLines(cmd.String("labels")),
		SourceDateEpoch: cmd.String("source-date-epoch"),
		CacheRepo:       cmd.String("cache-repo"),
		CacheScope:      cmd.String("cache-scope"),
		CachePush:       cmd.Bool("cache-push"),
		Mode:            domaincontainer.BuildOutputMode(cmd.String("mode")),
		ImageRef:        cmd.String("image-ref"),
		OutputDir:       cmd.String("output-dir"),
		DigestKey:       cmd.String("digest-key"),
	}
}

// splitLines splits a multiline flag value into trimmed, non-empty lines.
// Build args, secrets, and labels are newline-separated (NOT comma — a secret
// is "id=NAME,src=PATH", which contains a comma), matching how the workflow
// passes the multiline outputs of metadata / materialize-build-secrets.
func splitLines(s string) []string {
	var out []string

	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}

	return out
}
