// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
	"github.com/diggsweden/reusable-ci/internal/platform"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// debugSwapWarning is the Warning annotation text emitted when the
// --debug-allow-swap flag bypasses the swap policy. The exact tokens
// "--debug-allow-swap" and "swap" are pinned by the integration test.
//
//nolint:gosec // G101 false positive: the substring "key material" is operator-facing warning text, not a credential literal.
const debugSwapWarning = "swap policy bypassed via --debug-allow-swap — decrypted key material may be paged to disk. DO NOT USE FOR PRODUCTION RELEASES; see docs/verification.md#swap-policy."

// debugAllowSwapFlag is the operator's explicit opt-out from the
// swap-refusal policy. Flag-only by design so the override must
// reappear in argv each invocation — reviewable in workflow YAML
// during PR, discoverable via --help.
func debugAllowSwapFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "debug-allow-swap",
		Usage: "DEBUG ONLY — bypass the swap-refusal policy when /proc/swaps reports an active swap area; emits a loud Warning annotation. NOT FOR PRODUCTION RELEASES; the supported fix is to disable swap on the runner. See docs/verification.md#swap-policy.",
	}
}

// requireNoSwapWithWarning wraps the policy gate and, when the
// --debug-allow-swap override is active, emits a loud Warning
// annotation so the override decision lands in the CI log.
func requireNoSwapWithWarning(cmd *cli.Command) error {
	allow := cmd.Bool("debug-allow-swap")
	if err := safeexec.RequireNoSwap(allow); err != nil {
		return err
	}

	if allow {
		deps.Annotator(cmd).Warningf("%s", debugSwapWarning)
	}

	return nil
}

// signMethodFlags returns the per-invocation signing-method flag set
// shared by `release sign` and `release sbom-zip --sign`. Three
// methods exist (see docs/verification.md#signing-methods):
//
//   - gpg (default): long-lived OpenPGP key from $GPG_PRIVATE_KEY
//   - sigstore: keyless cosign + OIDC via the runner
//   - kms: cosign + explicit key reference (KMS URI / PKCS#11 / local key)
func signMethodFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "method",
			Sources: cli.EnvVars("SIGN_METHOD"),
			Value:   string(domain.DefaultSignMethod),
			Usage:   "signing backend: gpg (default; uses $GPG_PRIVATE_KEY), sigstore (keyless cosign + OIDC), or kms (cosign + --key)",
		},
		&cli.StringFlag{
			Name:    "key",
			Sources: cli.EnvVars("SIGN_KEY"),
			Usage:   "cosign --key reference for --method=kms: KMS URI (awskms:///alias/X, hashivault://transit/keys/X, gcpkms://..., azurekms://...), PKCS#11 URI, or local key-file path. Forbidden for --method=gpg/sigstore.",
		},
		&cli.StringFlag{
			Name:    "oidc-issuer",
			Sources: cli.EnvVars("SIGN_OIDC_ISSUER"),
			Usage:   "OIDC issuer URL for --method=sigstore (default: auto-detected — GitHub Actions / GitLab CI / $CI_SERVER_URL). Forbidden for --method=gpg/kms.",
		},
	}
}

// buildSigner constructs a release.Signer for the chosen method.
// GPG: in-process openpgp.Signer from $GPG_PRIVATE_KEY (armor never
// lands on disk; passphrase never reaches argv). Sigstore/KMS: cosign
// subprocess wrapper with method-specific argv.
//
// The closure returned by NewCosignSigner reads no extra env at
// SignFile time — the constructor captures everything (method,
// keyref, issuer) once, so a multi-artefact loop is configuration-
// stable.
func buildSigner(cmd *cli.Command, errOut io.Writer) (apprelease.Signer, domain.SignMethod, error) {
	method, err := domain.ParseSignMethod(cmd.String("method"))
	if err != nil {
		return nil, "", err
	}

	keyRef := cmd.String("key")
	oidcIssuer := cmd.String("oidc-issuer")

	if err := validateSignFlags(method, keyRef, oidcIssuer); err != nil {
		return nil, "", err
	}

	switch method {
	case domain.SignMethodGPG:
		return buildGPGSigner(method)
	case domain.SignMethodSigstore:
		return buildSigstoreSigner(method, oidcIssuer, errOut)
	case domain.SignMethodKMS:
		return buildKMSSigner(method, keyRef, errOut)
	default:
		// Unreachable: ParseSignMethod rejects anything else.
		return nil, "", fmt.Errorf("sign: method %q unsupported: %w", method, errs.ErrInvalidConfig)
	}
}

// buildGPGSigner reads GPG_PRIVATE_KEY + GPG_PASSPHRASE from env
// and constructs the in-process openpgp signer.
func buildGPGSigner(method domain.SignMethod) (apprelease.Signer, domain.SignMethod, error) {
	signer, err := openpgp.NewSignerFromArmor(
		[]byte(os.Getenv("GPG_PRIVATE_KEY")),
		os.Getenv("GPG_PASSPHRASE"),
	)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// buildSigstoreSigner constructs a cosign-keyless signer. An empty
// oidcIssuer auto-detects from the runner platform; empty + unknown
// platform falls back to cosign's own auto-detection (works on GHA;
// otherwise the operator must supply --oidc-issuer).
func buildSigstoreSigner(method domain.SignMethod, oidcIssuer string, errOut io.Writer) (apprelease.Signer, domain.SignMethod, error) {
	if oidcIssuer == "" {
		oidcIssuer = apprelease.DefaultOIDCIssuer(platform.Detect())
	}

	signer, err := apprelease.NewCosignSigner(cosign.New(), apprelease.CosignSignerInput{
		Method:     method,
		OIDCIssuer: oidcIssuer,
	}, errOut)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// buildKMSSigner constructs a cosign-KMS signer against keyRef.
func buildKMSSigner(method domain.SignMethod, keyRef string, errOut io.Writer) (apprelease.Signer, domain.SignMethod, error) {
	signer, err := apprelease.NewCosignSigner(cosign.New(), apprelease.CosignSignerInput{
		Method: method,
		KeyRef: keyRef,
	}, errOut)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// validateSignFlags checks the per-method invariants on --key and
// --oidc-issuer. The CLI rejects illegal combinations before any
// signing starts so the operator gets a clear "you mixed flags
// wrong" message instead of a downstream cosign-argv error.
func validateSignFlags(method domain.SignMethod, keyRef, oidcIssuer string) error {
	switch method {
	case domain.SignMethodGPG:
		if keyRef != "" {
			return fmt.Errorf("sign: --key is forbidden for --method=gpg (got %q): %w", keyRef, errs.ErrInvalidConfig)
		}

		if oidcIssuer != "" {
			return fmt.Errorf("sign: --oidc-issuer is forbidden for --method=gpg (got %q): %w", oidcIssuer, errs.ErrInvalidConfig)
		}
	case domain.SignMethodSigstore:
		if keyRef != "" {
			return fmt.Errorf("sign: --key is forbidden for --method=sigstore (keyless OIDC has no key) (got %q): %w", keyRef, errs.ErrInvalidConfig)
		}
	case domain.SignMethodKMS:
		if keyRef == "" {
			return fmt.Errorf("sign: --key is required for --method=kms (e.g. hashivault://transit/keys/release): %w", errs.ErrInvalidConfig)
		}

		if oidcIssuer != "" {
			return fmt.Errorf("sign: --oidc-issuer is forbidden for --method=kms (got %q): %w", oidcIssuer, errs.ErrInvalidConfig)
		}
	}

	return nil
}

func signCmd() *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{Name: "checksums-file", Sources: cli.EnvVars("CHECKSUMS_FILE"), Usage: "SHA256 manifest to sign (default: checksums.sha256)"},
		&cli.StringFlag{Name: "release-artifacts-dir", Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR"), Usage: "directory whose files are each signed alongside the manifest"},
		&cli.StringFlag{Name: "attach-artifacts", Sources: cli.EnvVars("ATTACH_ARTIFACTS"), Usage: "comma-separated globs for extra files to sign"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		debugAllowSwapFlag(),
	}, signMethodFlags()...)

	return &cli.Command{
		Name:  "sign",
		Usage: "detach-sign checksums.sha256, release artifacts, and attached artifacts. Method selectable via --method: gpg (default; .asc sidecar), sigstore (keyless cosign; .bundle sidecar), or kms (cosign + --key; .bundle sidecar).",
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			signer, method, err := buildSigner(cmd, os.Stderr)
			if err != nil {
				return err
			}

			// Swap policy applies only to the GPG branch — that's
			// the only path with decrypted key material in our
			// heap. Sigstore (ephemeral key, never on disk) and
			// KMS (key stays in the provider) are unaffected.
			if method == domain.SignMethodGPG {
				if err := requireNoSwapWithWarning(cmd); err != nil {
					return err
				}
			}

			return apprelease.SignArtifacts(ctx, signer, os.Stderr, apprelease.SignInput{
				ChecksumsFile:       cmd.String("checksums-file"),
				ReleaseArtifactsDir: cmd.String("release-artifacts-dir"),
				AttachArtifacts:     cmd.String("attach-artifacts"),
			})
		},
	}
}

func downloadArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "download-artifacts",
		Usage: "download release artifacts from the explicit artifact-transfer plan",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-transfer-plan-json", Sources: cli.EnvVars("ARTIFACT_TRANSFER_PLAN_JSON"), Usage: "typed transfer plan JSON listing which CI artifacts to download"},
			&cli.StringFlag{Name: "run-id", Sources: cli.EnvVars("GITHUB_RUN_ID", "CI_RUN_ID"), Usage: "CI run ID artifacts are downloaded from"},
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"), Usage: "\"owner/repo\" the run belongs to"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.DownloadArtifacts(ctx, github.NewArtifactDownloader(github.New()), os.Stderr, apprelease.DownloadArtifactsInput{
				ArtifactTransferPlanJSON: cmd.String("artifact-transfer-plan-json"),
				RunID:                    cmd.String("run-id"),
				Repository:               cmd.String("repository"),
			})
		},
	}
}

func checksumsCmd() *cli.Command {
	return &cli.Command{
		Name:  "checksums",
		Usage: "compute SHA256 over release artefacts, attached patterns, and SBOM layers",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output", Sources: cli.EnvVars("OUTPUT_FILE"),
				Usage: "manifest path (default: checksums.sha256)"},
			&cli.StringFlag{Name: "release-artifacts-dir", Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR"), Usage: "directory whose files are each hashed into the manifest"},
			&cli.StringFlag{Name: "attach-artifacts", Sources: cli.EnvVars("ATTACH_ARTIFACTS"),
				Usage: "comma-separated globs for additional files to checksum (paths kept verbatim)"},
			&cli.StringFlag{Name: "sbom-dir", Sources: cli.EnvVars("SBOM_DIR"), Usage: "directory of SBOM layer files to include in the manifest"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			n, err := apprelease.Checksums(os.Stderr, apprelease.ChecksumsInput{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				OutputFile:          cmd.String("output"),
				ReleaseArtifactsDir: cmd.String("release-artifacts-dir"),
				AttachArtifacts:     cmd.String("attach-artifacts"),
				SBOMDir:             cmd.String("sbom-dir"),
			})
			if err != nil {
				return err
			}

			if n == 0 {
				_, _ = fmt.Fprintln(os.Stderr, "No artefacts found to checksum.")
			}

			return nil
		},
	}
}

func sbomZipCmd() *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{
			Name:     "project-name",
			Required: true,
			Sources:  cli.EnvVars("PROJECT_NAME"),
			Usage:    "project slug used as the zip filename prefix",
		},
		&cli.StringFlag{
			Name:     "version",
			Required: true,
			Sources:  cli.EnvVars("VERSION"),
			Usage:    "release version baked into the zip filename",
		},
		&cli.BoolFlag{
			Name:    "sign",
			Sources: cli.EnvVars("SIGN_ARTIFACTS"),
			Usage:   "additionally sign the resulting zip using the method selected by --method",
		},
		&cli.StringFlag{
			Name:    "sbom-dir",
			Sources: cli.EnvVars("SBOM_DIR"),
			Usage:   "directory holding the SBOM layers to bundle",
		},
		debugAllowSwapFlag(),
	}, signMethodFlags()...)

	return &cli.Command{
		Name:  "sbom-zip",
		Usage: "bundle all SBOM layers into <project>-<version>-sboms.zip; optionally sign via --sign + --method",
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var signer apprelease.Signer

			if cmd.Bool("sign") {
				built, method, err := buildSigner(cmd, os.Stderr)
				if err != nil {
					return err
				}

				// GPG branch only: the decrypted key lives in our
				// heap during signing. Cosign methods keep the
				// key out of our address space entirely.
				if method == domain.SignMethodGPG {
					if err := requireNoSwapWithWarning(cmd); err != nil {
						return err
					}
				}

				signer = built
			}

			_, err := apprelease.CreateSBOMZip(ctx, signer, apprelease.SBOMZipInput{
				ProjectName:   cmd.String("project-name"),
				Version:       cmd.String("version"),
				SBOMDir:       cmd.String("sbom-dir"),
				SignArtifacts: cmd.Bool("sign"),
			}, os.Stderr)

			return err
		},
	}
}
