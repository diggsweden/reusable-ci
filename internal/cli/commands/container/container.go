// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package container wires `reusable-ci container ...` subcommands.
//
// Subcommand tree:
//
//   - validate.go — `validate <artifacts|containerfile|namespace>`
//   - image.go — `image <push>`
//   - manifest.go — `manifest <digest|inspect|merge|push>`
//   - container.go (this file) — flat ops: platform-plan, metadata,
//     write-digest-marker
//   - refhelpers.go — `ref <canonical|name|platform|resolve>` image-reference
//     projections + containerfile-arg-default
//   - tarball.go  — `extract-npm-tarball`
//   - binaries.go — `suffix-extracted-binaries`
//   - materialize_build_secrets.go — unpack the build-secrets envelope
//     into mode-0600 tmpfiles for buildah `--mount=type=secret` use
//
// `container --help` groups these into categories (Build & sign / Image
// metadata & manifests / Release promotion / Validation / Workflow plumbing)
// so a human sees the handful of verbs they type apart from the workflow
// plumbing. Categorization is centralized in New via withCategory; command
// paths are unaffected.
package container

import (
	"context"
	"errors"
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	gitadapter "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cmdmeta"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// flagRegistry is the shared flag name for the registry host, named once so the
// container package stays under goconst's literal budget.
const flagRegistry = "registry"

// flagDigest is the shared "digest" flag name, named once to stay under
// goconst's literal budget across the container package.
const flagDigest = "digest"

// flagFlavor is the shared "flavor" flag name (build-variant), named once to
// stay under goconst's literal budget across the container package.
const flagFlavor = "flavor"

// New returns the `container` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "container-image helpers (name resolution, manifests, namespace policy, tag/label metadata, …)",
		Commands: slices.Concat(
			cmdmeta.WithCategory("Build & sign", setupBuildahCmd(), buildCmd(), buildPushOCIImageCmd(), imageGroup(), signerImageGroup(), loginCmd(), logoutCmd(), signCmd(), attestCmd()),
			cmdmeta.WithCategory("Image metadata & manifests", refGroup(), metadataCmd(), containerfileArgDefaultCmd(), releaseLabelsCmd(), releaseIdentityMatchesCmd(), imageLabelsJSONCmd(), imageEvidenceCmd(), platformPlanCmd(), manifestGroup(), writeDigestMarkerCmd()),
			cmdmeta.WithCategory("Release promotion", ledgerGroup(), releaseImagesGroup(), baseImagesGroup()),
			cmdmeta.WithCategory("Validation", validateGroup()),
			cmdmeta.WithCategory("Workflow plumbing", materializeBuildSecretsCmd(), extractNPMTarballCmd(), suffixBinariesCmd()),
		),
	}
}

func loginCmd() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "write registry credentials to the shared OCI auth config used by docker, podman, buildah, skopeo, and cosign",
		Description: `Writes {"auths":{...}} to $REGISTRY_AUTH_FILE / $DOCKER_CONFIG/config.json /
~/.docker/config.json (first that applies). The password is read from a file or
stdin or $REGISTRY_PASSWORD — never argv — and the file is written 0600.

EXAMPLES:
   echo "$TOKEN" | reusable-ci container login --registry ghcr.io --registry-username "$GITHUB_ACTOR" --registry-password-file -
   reusable-ci container login --registry forgejo.example.com --registry-username bot   # password from $REGISTRY_TOKEN / $REGISTRY_PASSWORD`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRegistry, Value: domaincontainer.DefaultRegistry, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry host (e.g. ghcr.io, forgejo.example.com)"},
			&cli.StringFlag{Name: flagServerURL, Usage: "forge/server URL used to derive the registry host when --registry is empty or omitted"},
			regflags.Username(),
			regflags.PasswordFile(),
			&cli.StringFlag{Name: flagAuthFile, Usage: "override the auth config path (default: $REGISTRY_AUTH_FILE, else $DOCKER_CONFIG/config.json, else ~/.docker/config.json)"},
			&cli.BoolFlag{Name: "create-auth-file", Sources: cli.EnvVars("REGISTRY_CREATE_AUTH_FILE"), Usage: "when --auth-file is empty, create a fresh job-local auth file under $RUNNER_TEMP"},
			&cli.BoolFlag{Name: "export-env", Sources: cli.EnvVars("REGISTRY_EXPORT_AUTH_FILE_ENV"), Usage: "append REGISTRY_AUTH_FILE=<auth-file> to --env-file; requires an explicit or created auth file"},
			&cli.StringFlag{Name: "env-file", Sources: cli.EnvVars("FORGEJO_ENV", "GITHUB_ENV"), Usage: "runner env file used with --export-env"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				registry, err := registryForLogin(cmd.String(flagRegistry), cmd.String(flagServerURL), cmd.IsSet(flagRegistry))
				if err != nil {
					return err
				}

				username := strings.TrimSpace(cmd.String(regflags.FlagUsername))

				authFile, err := authFileForLogin(cmd.String(flagAuthFile), cmd.Bool("create-auth-file"))
				if err != nil {
					return err
				}

				password, err := regflags.FileOrEnv(cmd.String(regflags.FlagPasswordFile), releaseImagesDefaultEnvVar, "REGISTRY_PASSWORD")
				if err != nil {
					return err
				}

				username, password, err = registryLoginCredentials(registry, username, password)
				if err != nil {
					return err
				}

				if password == "" {
					return errs.CredentialRequired(errs.Credential{What: regflags.CredPassword, Flag: regflags.FlagPasswordFile, Env: releaseImagesDefaultEnvVar})
				}

				if err := appcontainer.RegistryLogin(os.Stderr, appcontainer.RegistryLoginInput{
					Registry: registry,
					Username: username,
					Password: password,
					AuthFile: authFile,
				}); err != nil {
					return err
				}

				if err := publishAuthFilePath(ctx, cmd, dep, authFile); err != nil {
					return err
				}

				return maybeExportRegistryAuthFile(cmd.Bool("export-env"), cmd.String("env-file"), authFile)
			})
		},
	}
}

// publishAuthFilePath reports where the auth file landed, as a step output for
// later steps to pick up.
//
// It is a convenience, not the point of logging in, and that distinction decides
// how a failure is handled. GitHub and Forgejo runners always provide an output
// file, so the write never fails there. GitLab provides none unless the pipeline
// nominates one in $CI_OUTPUT — and returning that error made a *successful*
// login exit non-zero on GitLab for every pipeline that never asked for an
// output: the credential was written, the login had worked, and the job failed
// anyway.
//
// So a missing sink degrades to a notice, the way an unmet capability does
// elsewhere, and --export-env remains the explicit way to hand the path onward.
// Any other error still fails the command, because that is a real write problem
// rather than a platform difference.
func publishAuthFilePath(ctx context.Context, cmd *cli.Command, dep *deps.Deps, authFile string) error {
	err := dep.OutputSink.Set(ctx, flagAuthFile, authFile)
	if err == nil {
		return nil
	}

	if !errors.Is(err, errs.ErrUsage) {
		return err
	}

	deps.Annotator(cmd).Noticef("logged in; the %s path was not published as a step output — %v", flagAuthFile, err)

	return nil
}

func registryForLogin(registry, serverURL string, registrySet bool) (string, error) {
	registry = strings.TrimSpace(registry)

	serverURL = strings.TrimSpace(serverURL)
	if serverURL != "" && (!registrySet || registry == "") {
		return registryHostFromServerURL(serverURL)
	}

	if registry != "" {
		return registry, nil
	}

	return domaincontainer.DefaultRegistry, nil
}

func registryHostFromServerURL(raw string) (string, error) {
	host, err := domaincontainer.RegistryHost(raw)
	if err != nil {
		return "", fmt.Errorf("container login: --server-url: %w", err)
	}

	return host, nil
}

func authFileForLogin(authFile string, create bool) (string, error) {
	authFile = strings.TrimSpace(authFile)
	if authFile != "" || !create {
		return authFile, nil
	}

	tmp, err := os.CreateTemp(runnerTempDir(), "registry-auth.*.json")
	if err != nil {
		return "", fmt.Errorf("container login: create isolated auth file: %w", err)
	}

	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("container login: close isolated auth file %s: %w", path, err)
	}

	return path, nil
}

func runnerTempDir() string {
	if dir := runcontext.TempDir().Resolve(os.Getenv); strings.TrimSpace(dir) != "" {
		return dir
	}

	return os.TempDir()
}

func maybeExportRegistryAuthFile(export bool, envFile, authFile string) error {
	if !export {
		return nil
	}

	if strings.TrimSpace(authFile) == "" {
		return fmt.Errorf("container login: --export-env requires --auth-file or --create-auth-file: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(envFile) == "" {
		return fmt.Errorf("container login: --export-env requires --env-file or $FORGEJO_ENV/$GITHUB_ENV: %w", errs.ErrUsage)
	}

	if err := cliio.AppendLines(envFile, "REGISTRY_AUTH_FILE="+authFile); err != nil {
		return fmt.Errorf("container login: write REGISTRY_AUTH_FILE: %w", err)
	}

	return nil
}

// forgeRegistryCreds returns the detected forge's runner-injected registry
// username and token when a resolver exists and the login target is that
// forge's own registry; otherwise empty credentials. Resolver failures are
// returned so a broken runtime context cannot silently fall back to a less
// specific credential source.
func forgeRegistryCreds(registry string) (string, string, error) {
	resolver, ok := deps.RegistryAuthForDetected()
	if !ok {
		return "", "", nil
	}

	auth, err := resolver.ResolveRegistryAuth()
	if err != nil {
		return "", "", fmt.Errorf("resolve detected forge registry credentials: %w", err)
	}

	if !auth.MatchesRegistry(registry) {
		return "", "", nil
	}

	return auth.Username, auth.Token, nil
}

// registryLoginCredentials falls back to runner-injected forge credentials only
// when no explicit password was supplied and the target is that forge's own
// registry. Explicit username/password values always win.
func registryLoginCredentials(registry, username, password string) (string, string, error) {
	if password != "" {
		return username, password, nil
	}

	forgeUser, forgeToken, err := forgeRegistryCreds(registry)
	if err != nil || forgeToken == "" {
		return username, "", err
	}

	if username == "" {
		username = forgeUser
	}

	return username, forgeToken, nil
}

func logoutCmd() *cli.Command {
	return &cli.Command{
		Name:  "logout",
		Usage: "remove a registry's credential from the shared OCI auth config (the inverse of login) — for long-lived self-hosted runners where the auth file outlives the job",
		Description: `Deletes the {"auths":{"<registry>":…}} entry from $REGISTRY_AUTH_FILE /
$DOCKER_CONFIG/config.json / ~/.docker/config.json (first that applies),
preserving every other registry's credential. Idempotent: a missing config or
an absent entry is a successful no-op, so it is safe under "if: always()".

EXAMPLES:
   reusable-ci container logout --registry ghcr.io
   reusable-ci container logout   # defaults to $CONTAINER_REGISTRY, else ` + domaincontainer.DefaultRegistry,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRegistry, Value: domaincontainer.DefaultRegistry, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry host to log out of (e.g. ghcr.io, forgejo.example.com)"},
			&cli.StringFlag{Name: flagAuthFile, Usage: "override the auth config path (default: $REGISTRY_AUTH_FILE, else $DOCKER_CONFIG/config.json, else ~/.docker/config.json)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appcontainer.RegistryLogout(os.Stderr, appcontainer.RegistryLogoutInput{
				Registry: cmd.String(flagRegistry),
				AuthFile: cmd.String(flagAuthFile),
			})
		},
	}
}

func writeDigestMarkerCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-digest-marker",
		Usage: "write a validated digest marker file for manifest merging",
		Description: `Workflow-internal: each per-arch build job records its pushed digest here so
` + "`container manifest merge`" + ` can assemble the multi-arch index.

EXAMPLE:
   DIGEST=sha256:abc... DIGESTS_DIR=/tmp/digests reusable-ci container write-digest-marker`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagDigest, Sources: cli.EnvVars("DIGEST"), Usage: "sha256:… digest written into the per-arch marker file"},
			&cli.StringFlag{Name: "digests-dir", Value: domaincontainer.DefaultDigestsDir, Sources: cli.EnvVars("DIGESTS_DIR"), Usage: "directory where the marker file is written"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			path, err := appcontainer.WriteDigestMarker(appcontainer.WriteDigestMarkerInput{
				Digest:     cmd.String(flagDigest),
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
		Description: `EXAMPLE:
   reusable-ci container platform-plan --platforms "linux/amd64,linux/arm64"`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "platforms", Sources: cli.EnvVars("PLATFORMS"), Usage: "comma/space/newline-separated build platforms (linux/amd64,linux/arm64)"},
			&cli.StringFlag{Name: flagPlatform, Sources: cli.EnvVars("PLATFORM"), Usage: "single build platform; falls back to the first entry of --platforms"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				_, err := appcontainer.PlatformPlan(ctx, dep.OutputSink, os.Stderr, appcontainer.PlatformPlanInput{
					Platforms: cmd.String("platforms"),
					Platform:  cmd.String(flagPlatform),
				})

				return err
			})
		},
	}
}

// planScopeMetadata is the plan-file scope of `container metadata` in
// $REUSABLE_CI_PLAN (flag > plan > env > default).
const planScopeMetadata = "container metadata"

func metadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "compute image tags + OCI labels from declarative tag rules",
		Description: "Reads TAG_RULES (newline csv lines), evaluates them " +
			"against the resolved provider event context, and writes tags/labels/version/json " +
			"to the platform output sink. Every flag may also be fed from the " +
			"$REUSABLE_CI_PLAN plan file under the \"container metadata\" scope " +
			"(flag > plan > env > default).\n\n" +
			"EXAMPLES:\n" +
			"   # Semver tags + OCI labels from a tag rule\n" +
			"   reusable-ci container metadata --image-name ghcr.io/org/app \\\n" +
			"     --tag-rules \"type=semver,pattern={{version}}\" --emit-labels",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagImageName, Required: true, Sources: planfile.Vars(planScopeMetadata, flagImageName, "IMAGE_NAME"),
				Usage: "single base image ref, e.g. ghcr.io/owner/repo"},
			&cli.StringFlag{Name: "tag-rules", Sources: planfile.Vars(planScopeMetadata, "tag-rules", "TAG_RULES"),
				Usage: "newline-separated csv tag-rule lines"},
			&cli.StringFlag{Name: flagFlavor, Sources: planfile.Vars(planScopeMetadata, flagFlavor, "FLAVOR"),
				Usage: "only latest=false is honoured; other entries are refused"},
			&cli.BoolFlag{Name: "emit-labels", Sources: planfile.Vars(planScopeMetadata, "emit-labels", "EMIT_LABELS"),
				Usage: "also emit org.opencontainers.image.* labels"},
			&cli.StringFlag{Name: "oci-description", Sources: planfile.Vars(planScopeMetadata, "oci-description", "OCI_DESCRIPTION"),
				Usage: "override for org.opencontainers.image.description"},
			&cli.StringFlag{Name: "oci-license", Sources: planfile.Vars(planScopeMetadata, "oci-license", "OCI_LICENSE"),
				Usage: "override for org.opencontainers.image.licenses (SPDX id)"},
			&cli.StringFlag{Name: "source-ref-name", Sources: planfile.Vars(planScopeMetadata, "source-ref-name", "SOURCE_REF_NAME"), Usage: "explicit source ref name for release metadata"},
			&cli.StringFlag{Name: "source-ref-type", Sources: planfile.Vars(planScopeMetadata, "source-ref-type", "SOURCE_REF_TYPE"), Usage: "explicit source ref type (tag, branch, pr, other)"},
			&cli.StringFlag{Name: "source-revision", Sources: planfile.Vars(planScopeMetadata, "source-revision", "SOURCE_REVISION"), Usage: "explicit released commit SHA"},
			&cli.StringFlag{Name: "source-date-epoch", Sources: planfile.Vars(planScopeMetadata, "source-date-epoch", "SOURCE_DATE_EPOCH"), Usage: "Unix timestamp for deterministic OCI created labels; defaults to HEAD commit time"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				createdAt, err := metadataCreatedAt(ctx, cmd.String("source-date-epoch"), cmd.Bool("emit-labels"))
				if err != nil {
					return err
				}

				_, err = appcontainer.ComputeMetadata(ctx, dep.Provider, dep.RepoMetadataFetcher(), dep.OutputSink, dep.ManifestSink, appcontainer.ComputeMetadataInput{
					ImageName:      cmd.String(flagImageName),
					TagRules:       cmd.String("tag-rules"),
					Flavor:         cmd.String(flagFlavor),
					EmitLabels:     cmd.Bool("emit-labels"),
					Description:    cmd.String("oci-description"),
					License:        cmd.String("oci-license"),
					SourceRefName:  cmd.String("source-ref-name"),
					SourceRefType:  provider.RefType(cmd.String("source-ref-type")),
					SourceRevision: cmd.String("source-revision"),
					Now:            createdAt,
				})

				return err
			})
		},
	}
}

func metadataCreatedAt(ctx context.Context, raw string, needed bool) (time.Time, error) {
	if !needed {
		return time.Time{}, nil
	}

	if strings.TrimSpace(raw) == "" {
		var err error

		raw, err = gitadapter.New().CommitUnixTime(ctx, "HEAD")
		if err != nil {
			return time.Time{}, fmt.Errorf("derive OCI creation time from HEAD: %w", err)
		}
	}

	epoch, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || epoch < 0 {
		return time.Time{}, fmt.Errorf("source-date-epoch must be non-negative Unix seconds (got %q): %w", raw, errs.ErrUsage)
	}

	return time.Unix(epoch, 0).UTC(), nil
}
