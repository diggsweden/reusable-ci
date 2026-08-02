// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func releaseImageGroup() *cli.Command {
	return &cli.Command{
		Name:  "release-image",
		Usage: "single release-image verification helpers",
		Description: `Typed helpers for one digest-pinned release image. This group
keeps consumer-specific build, smoke, and flavor orchestration in the caller
while reusable-ci owns the shared signature/SBOM/provenance and safe
re-attestation predicates.`,
		Commands: []*cli.Command{
			releaseImageVerifyExistingCmd(),
		},
	}
}

func releaseImageVerifyExistingCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdVerifyExisting,
		Usage: "verify an existing digest-pinned release image before reusing it",
		Description: `Checks Cosign signature, CycloneDX SBOM attestation, and SLSA
provenance fields for a digest-pinned release image. With --allow-reattest, a
provenance mismatch may still return status "reattestable" when the image is
signed, SBOM-attested, and its OCI release labels match the expected identity.
The command prints one status token to stdout on success: verified or
reattestable.`,
		Flags: append(baseImagesPublicKeyFlags(),
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "digest-pinned image ref to verify"},
			&cli.StringFlag{Name: "expected-tag", Required: true, Sources: cli.EnvVars("RELEASE_TAG"), Usage: "release tag expected in SLSA workflow externalParameters.workflow.ref"},
			&cli.StringFlag{Name: "expected-commit", Required: true, Sources: cli.EnvVars("RELEASE_SHA"), Usage: "source commit expected in SLSA resolvedDependencies[].digest.gitCommit"},
			&cli.StringFlag{Name: "expected-source", Required: true, Sources: cli.EnvVars("EXPECTED_SOURCE"), Usage: "source repository URL expected in SLSA workflow externalParameters.workflow.repository"},
			&cli.StringFlag{Name: "expected-workflow", Required: true, Sources: cli.EnvVars("EXPECTED_WORKFLOW"), Usage: "workflow path expected in SLSA workflow externalParameters.workflow.path"},
			&cli.StringFlag{Name: "expected-base-ref", Sources: cli.EnvVars("EXPECTED_BASE_REF"), Usage: "optional digest-pinned base ref expected in SLSA base lineage"},
			&cli.StringFlag{Name: "expected-base-input-id", Sources: cli.EnvVars("EXPECTED_BASE_INPUT_ID"), Usage: "optional sha256 base input ID expected in SLSA base lineage"},
			&cli.BoolFlag{Name: "allow-reattest", Sources: cli.EnvVars("ALLOW_REATTEST"), Usage: "allow signed/SBOM-attested images with matching OCI identity labels to be re-attested"},
			&cli.StringFlag{Name: "identity-version", Usage: "OCI org.opencontainers.image.version expected for --allow-reattest; defaults to --expected-tag"},
			&cli.StringFlag{Name: "identity-ref-name", Usage: "OCI org.opencontainers.image.ref.name expected for --allow-reattest; defaults to --expected-tag"},
			&cli.StringFlag{Name: "identity-source", Usage: "OCI org.opencontainers.image.source expected for --allow-reattest; defaults to --expected-source"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "Docker/containers auth config for registry label reads and cosign verification"}),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := validateReleaseImageCLIInput(cmd); err != nil {
				return err
			}

			registry := ociregistry.New()
			if authFile := cmd.String(flagAuthFile); authFile != "" {
				registry = ociregistry.WithAuthFile(authFile)
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return withReleaseImageDockerConfig(cmd.String(flagAuthFile), func() error {
					result, err := appcontainer.VerifyExistingReleaseImage(ctx,
						cosign.NewIsolated("DOCKER_CONFIG"),
						registry,
						os.Stderr,
						releaseImageVerifyExistingInputFromCmd(cmd),
					)
					if err != nil {
						return err
					}

					_, _ = fmt.Fprintln(os.Stdout, result.Status)

					return dep.OutputSink.Set(ctx, "status", result.Status)
				})
			})
		},
	}
}

func validateReleaseImageCLIInput(cmd *cli.Command) error {
	if err := validateReleaseImagePublicKey(cmd.String("cosign-public-key-path"), cmd.String("cosign-public-key-sha256")); err != nil {
		return err
	}

	if workflow := cmd.String("expected-workflow"); unsafeWorkflowPath(workflow) {
		return fmt.Errorf("release image verify: unsafe expected-workflow path: %s: %w", workflow, errs.ErrUsage)
	}

	return nil
}

func validateReleaseImagePublicKey(path, want string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("release image verify: cosign-public-key-path is required: %w", errs.ErrUsage)
	}

	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}

	if !baseImagesHex64RE.MatchString(want) {
		return fmt.Errorf("release image verify: cosign-public-key-sha256 must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("release image verify: Cosign public key is missing, not a regular file, or a symlink: %s: %w", path, errs.ErrMissingInput)
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected public key path; only public key material is read.
	if err != nil {
		return fmt.Errorf("release image verify: read Cosign public key %s: %w", path, err)
	}

	sum := sha256.Sum256(body)

	actual := hex.EncodeToString(sum[:])
	if actual != want {
		return fmt.Errorf("release image verify: Cosign public key digest does not match expected trust root\n  path:     %s\n  expected: %s\n  actual:   %s: %w", path, want, actual, errs.ErrValidation)
	}

	return nil
}

func releaseImageVerifyExistingInputFromCmd(cmd *cli.Command) appcontainer.ReleaseImageVerifyExistingInput {
	return appcontainer.ReleaseImageVerifyExistingInput{
		Ref:                     cmd.String(flagRef),
		CosignPublicKey:         cmd.String("cosign-public-key-path"),
		ExpectedTag:             cmd.String("expected-tag"),
		ExpectedCommit:          cmd.String("expected-commit"),
		ExpectedSource:          cmd.String("expected-source"),
		ExpectedWorkflow:        cmd.String("expected-workflow"),
		ExpectedBaseRef:         cmd.String("expected-base-ref"),
		ExpectedBaseInputID:     cmd.String("expected-base-input-id"),
		AllowReattest:           cmd.Bool("allow-reattest"),
		ExpectedIdentityVersion: cmd.String("identity-version"),
		ExpectedIdentityRefName: cmd.String("identity-ref-name"),
		ExpectedIdentitySource:  cmd.String("identity-source"),
	}
}

func withReleaseImageDockerConfig(authFile string, fn func() error) error {
	authFile = strings.TrimSpace(authFile)
	if authFile == "" {
		return fn()
	}

	dir, err := os.MkdirTemp(runnerTempDir(), "reusable-ci-release-image-docker-config-*")
	if err != nil {
		return fmt.Errorf("release image verify: create Docker config dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil { //nolint:gosec // 0o700: a directory needs the owner execute bit; it stays owner-only.
		return fmt.Errorf("release image verify: chmod Docker config dir: %w", chmodErr)
	}

	body, err := os.ReadFile(authFile) //nolint:gosec // caller-selected registry auth file.
	if err != nil {
		return fmt.Errorf("release image verify: read auth file %s: %w", authFile, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil { //nolint:gosec // path is inside the fresh MkdirTemp dir created above.
		return fmt.Errorf("release image verify: write Docker config for cosign: %w", err)
	}

	oldDockerConfig, hadDockerConfig := os.LookupEnv("DOCKER_CONFIG")

	if err := os.Setenv("DOCKER_CONFIG", dir); err != nil {
		return fmt.Errorf("release image verify: set DOCKER_CONFIG: %w", err)
	}

	defer restoreEnv("DOCKER_CONFIG", oldDockerConfig, hadDockerConfig)

	return fn()
}
