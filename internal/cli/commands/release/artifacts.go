// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cmdmeta"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
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

// signSources builds a flag source chain that resolves from the
// $REUSABLE_CI_PLAN scope first when the calling verb is plan-scoped;
// an empty planScope keeps the plain env chain (`release sbom-zip` is
// not plan-wired). With no env names and no scope it yields an empty
// chain, identical to leaving Sources unset.
func signSources(planScope, key string, envNames ...string) cli.ValueSourceChain {
	if planScope == "" {
		return cli.EnvVars(envNames...)
	}

	return planfile.Vars(planScope, key, envNames...)
}

// signMethodFlags returns the per-invocation signing-method flag set
// shared by `release sign` and `release sbom-zip --sign`. Three
// methods exist (see docs/verification.md#signing-methods):
//
//   - gpg (default): long-lived OpenPGP key from --private-key-file or
//     $GPG_PRIVATE_KEY
//   - sigstore: keyless cosign + OIDC via the runner
//   - kms: cosign + explicit key reference (KMS URI / PKCS#11 / local key)
//
// A non-empty planScope additionally resolves every flag from that
// $REUSABLE_CI_PLAN scope (flag > plan > env > default).
func signMethodFlags(planScope string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "method",
			Sources: signSources(planScope, "method", "SIGN_METHOD"),
			Value:   string(domainrelease.DefaultSignMethod),
			Usage:   "signing backend: gpg (default; key from --private-key-file or $GPG_PRIVATE_KEY), sigstore (keyless cosign + OIDC), or kms (cosign + --key)",
		},
		&cli.StringFlag{
			Name:    flagKey,
			Sources: signSources(planScope, flagKey, "SIGN_KEY"),
			Usage:   "cosign --key reference for --method=kms: KMS URI (awskms:///alias/X, hashivault://transit/keys/X, gcpkms://..., azurekms://...), PKCS#11 URI, or local key-file path. Forbidden for --method=gpg/sigstore.",
		},
		&cli.StringFlag{
			Name:    flagOIDCIssuer,
			Sources: signSources(planScope, flagOIDCIssuer, "SIGN_OIDC_ISSUER"),
			Usage:   "OIDC issuer URL for --method=sigstore (default: auto-detected — GitHub Actions / GitLab CI / $CI_SERVER_URL). Forbidden for --method=gpg/kms.",
		},
		&cli.StringFlag{
			Name:    flagFulcioURL,
			Sources: signSources(planScope, flagFulcioURL, "SIGN_FULCIO_URL"),
			Usage:   "certificate authority for --method=sigstore (default: public Sigstore). Set this for a self-hosted Sigstore: --oidc-issuer alone does not redirect it, so the token is minted by your issuer and then presented to the public CA. Forbidden for --method=gpg/kms.",
		},
		&cli.StringFlag{
			Name:    flagTrustedRoot,
			Sources: signSources(planScope, flagTrustedRoot, "SIGN_TRUSTED_ROOT"),
			Usage:   "trust material cosign verifies the new signature against, as produced by `cosign trusted-root create` (default: cosign's own). Required for a self-hosted CA: cosign verifies the certificate it was just issued and cannot learn a private root any other way. Forbidden for --method=gpg/kms.",
		},
		&cli.StringFlag{
			Name:    flagRekorURL,
			Sources: signSources(planScope, flagRekorURL, "SIGN_REKOR_URL"),
			Usage:   "transparency log for --method=sigstore (default: public Sigstore). Where to publish, not whether: see REUSABLE_CI_COSIGN_TRANSPARENCY for that. Forbidden for --method=gpg/kms.",
		},
		&cli.StringFlag{
			Name:    flagPrivateKeyFile,
			Sources: signSources(planScope, flagPrivateKeyFile),
			Usage:   "path to the armored GPG private key for --method=gpg (\"-\" for stdin; defaults to $GPG_PRIVATE_KEY). Lets the key be passed via file/stdin instead of the environment. Forbidden for --method=sigstore/kms.",
		},
		&cli.StringFlag{
			Name:    flagPassphraseFile,
			Sources: signSources(planScope, flagPassphraseFile),
			Usage:   "path to the GPG passphrase for --method=gpg (\"-\" for stdin; defaults to $GPG_PASSPHRASE). Forbidden for --method=sigstore/kms.",
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
// keyref, issuer) once, so a multi-artifact loop is configuration-
// stable.
func buildSigner(cmd *cli.Command, errOut io.Writer) (apprelease.Signer, domainrelease.SignMethod, error) {
	method, err := domainrelease.ParseSignMethod(cmd.String("method"))
	if err != nil {
		return nil, "", err
	}

	keyRef := cmd.String(flagKey)
	oidcIssuer := cmd.String(flagOIDCIssuer)
	keyFile := cmd.String(flagPrivateKeyFile)
	passFile := cmd.String(flagPassphraseFile)

	if err := validateSignFlags(method, keyRef, oidcIssuer, keyFile, passFile); err != nil {
		return nil, "", err
	}

	switch method {
	case domainrelease.SignMethodGPG:
		return buildGPGSigner(cmd, method)
	case domainrelease.SignMethodSigstore:
		if apprelease.KeylessNeedsIssuer(oidcIssuer, deps.CapabilitiesForDetected().KeylessOIDC) {
			deps.Annotator(cmd).Warningf(
				"keyless (sigstore) signing has no OIDC issuer on %s — cosign may fail; "+
					"use --method=gpg or --method=kms, or pass --oidc-issuer explicitly",
				deps.DescriberForDetected().Describe().DisplayName)
		}

		return buildSigstoreSigner(method, oidcIssuer, signflags.ReadEndpoints(cmd), errOut)
	case domainrelease.SignMethodKMS:
		return buildKMSSigner(method, keyRef, errOut)
	default:
		// Unreachable: ParseSignMethod rejects anything else.
		return nil, "", fmt.Errorf("sign: method %q unsupported: %w", method, errs.ErrInvalidConfig)
	}
}

// buildGPGSigner resolves the GPG private key and passphrase and builds
// the in-process openpgp signer. Each is read from its --*-file flag
// ("-" for stdin) when given, otherwise from $GPG_PRIVATE_KEY /
// $GPG_PASSPHRASE — so the key can be kept out of the environment
// (clig.dev §Environment variables: prefer credential files/stdin for
// secrets), matching the input contract `release gpg import` already uses.
func buildGPGSigner(cmd *cli.Command, method domainrelease.SignMethod) (apprelease.Signer, domainrelease.SignMethod, error) {
	privateKey, err := secret.Resolve(cmd.String(flagPrivateKeyFile), "GPG_PRIVATE_KEY")
	if err != nil {
		return nil, "", err
	}

	if privateKey == "" {
		return nil, "", errs.CredentialRequired(errs.Credential{What: credentialGPGPrivateKey, Flag: flagPrivateKeyFile, Env: "GPG_PRIVATE_KEY"})
	}

	passphrase, err := secret.Resolve(cmd.String(flagPassphraseFile), "GPG_PASSPHRASE")
	if err != nil {
		return nil, "", err
	}

	signer, err := openpgp.NewSignerFromArmor([]byte(privateKey), passphrase)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// buildSigstoreSigner constructs a cosign-keyless signer. An empty
// oidcIssuer auto-detects from the runner platform; empty + unknown
// platform falls back to cosign's own auto-detection (works on GHA;
// otherwise the operator must supply --oidc-issuer).
func buildSigstoreSigner(method domainrelease.SignMethod, oidcIssuer string, endpoints signflags.Endpoints, errOut io.Writer) (apprelease.Signer, domainrelease.SignMethod, error) {
	if oidcIssuer == "" {
		oidcIssuer = apprelease.DefaultOIDCIssuer(deps.DescriberForDetected())
	}

	signer, err := apprelease.NewCosignSigner(cosign.New(), apprelease.CosignSignerInput{
		Method:          method,
		OIDCIssuer:      oidcIssuer,
		FulcioURL:       endpoints.FulcioURL,
		RekorURL:        endpoints.RekorURL,
		TrustedRootPath: endpoints.TrustedRootPath,
	}, errOut)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// buildKMSSigner constructs a cosign-KMS signer against keyRef.
func buildKMSSigner(method domainrelease.SignMethod, keyRef string, errOut io.Writer) (apprelease.Signer, domainrelease.SignMethod, error) {
	adapter := cosign.New()
	if allow := provenanceSignAllow(keyRef); allow != nil {
		adapter = cosign.NewIsolated(allow...)
	}

	signer, err := apprelease.NewCosignSigner(adapter, apprelease.CosignSignerInput{
		Method: method,
		KeyRef: keyRef,
	}, errOut)
	if err != nil {
		return nil, "", err
	}

	return signer, method, nil
}

// signFlagRule is one cell of the sign-method flag matrix: a flag the
// method requires or forbids, plus the hint that tells the operator why.
// An empty why prints nothing.
type signFlagRule struct {
	flag string
	why  string
}

// whyGPGOnly is the hint on every rule that exists because the GPG key
// inputs have no meaning for the cosign-backed methods.
const whyGPGOnly = "GPG only"

// signFlagRules is the per-method flag contract, as data rather than as
// branches: each method names the flags it requires and the flags it
// refuses. Written out, the matrix is the documentation — you can see that
// only kms takes a --key, and that --private-key-file/--passphrase-file are
// GPG's alone, without reading any control flow.
//
// A method absent from the map constrains nothing. A new sign method is a
// row here, not a new branch plus a new test.
func signFlagRules() map[domainrelease.SignMethod]struct {
	require []signFlagRule
	forbid  []signFlagRule
} {
	return map[domainrelease.SignMethod]struct {
		require []signFlagRule
		forbid  []signFlagRule
	}{
		domainrelease.SignMethodGPG: {
			forbid: []signFlagRule{
				{flag: flagKey},
				{flag: flagOIDCIssuer},
			},
		},
		domainrelease.SignMethodSigstore: {
			forbid: []signFlagRule{
				{flag: flagKey, why: "keyless OIDC has no key"},
				{flag: flagPrivateKeyFile, why: whyGPGOnly},
				{flag: flagPassphraseFile, why: whyGPGOnly},
			},
		},
		domainrelease.SignMethodKMS: {
			require: []signFlagRule{
				{flag: flagKey, why: "e.g. hashivault://transit/keys/release"},
			},
			forbid: []signFlagRule{
				{flag: flagOIDCIssuer},
				{flag: flagPrivateKeyFile, why: whyGPGOnly},
				{flag: flagPassphraseFile, why: whyGPGOnly},
			},
		},
	}
}

// parenthesise renders a rule's hint as " (why)", or "" when there is none.
func parenthesise(why string) string {
	if why == "" {
		return ""
	}

	return " (" + why + ")"
}

// validateSignFlags checks the per-method invariants on --key,
// --oidc-issuer, and the GPG --private-key-file/--passphrase-file inputs
// against signFlagRules(). The CLI rejects illegal combinations before any
// signing starts so the operator gets a clear "you mixed flags wrong"
// message instead of a downstream cosign-argv error.
func validateSignFlags(method domainrelease.SignMethod, keyRef, oidcIssuer, keyFile, passFile string) error {
	given := map[string]string{
		flagKey:            keyRef,
		flagOIDCIssuer:     oidcIssuer,
		flagPrivateKeyFile: keyFile,
		flagPassphraseFile: passFile,
	}

	rules := signFlagRules()[method]

	for _, rule := range rules.require {
		if given[rule.flag] == "" {
			return fmt.Errorf("sign: --%s is required for --method=%s%s: %w",
				rule.flag, method, parenthesise(rule.why), errs.ErrInvalidConfig)
		}
	}

	for _, rule := range rules.forbid {
		if value := given[rule.flag]; value != "" {
			return fmt.Errorf("sign: --%s is forbidden for --method=%s%s (got %q): %w",
				rule.flag, method, parenthesise(rule.why), value, errs.ErrInvalidConfig)
		}
	}

	return nil
}

// planScopeSign is the plan-file scope of `release sign` in
// $REUSABLE_CI_PLAN (flag > plan > env > default).
const planScopeSign = "release sign"

func signCmd() *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{Name: flagChecksumsFile, Sources: planfile.Vars(planScopeSign, flagChecksumsFile, "CHECKSUMS_FILE"), Usage: "SHA256 manifest to sign (default: checksums.sha256)"},
		&cli.BoolFlag{Name: "no-checksums-file", Sources: planfile.Vars(planScopeSign, "no-checksums-file"), Usage: "do not sign the default or configured checksums file"},
		&cli.BoolFlag{Name: "checksums-from-manifest", Sources: planfile.Vars(planScopeSign, "checksums-from-manifest"), Usage: "use and validate the single checksums file from --manifest before signing it"},
		&cli.StringFlag{Name: flagAssembly, Sources: planfile.Vars(planScopeSign, flagAssembly, "RELEASE_ASSEMBLY"), Usage: "release assembly manifest to sign exactly"},
		&cli.StringFlag{Name: flagReleaseArtifactsDir, Sources: planfile.Vars(planScopeSign, flagReleaseArtifactsDir, "RELEASE_ARTIFACTS_DIR"), Usage: "directory whose files are each signed alongside the manifest"},
		&cli.BoolFlag{Name: "no-release-artifacts-dir", Sources: planfile.Vars(planScopeSign, "no-release-artifacts-dir"), Usage: "do not sign the default or configured release artifacts directory"},
		&cli.StringFlag{Name: flagAttachArtifacts, Sources: planfile.Vars(planScopeSign, flagAttachArtifacts, "ATTACH_ARTIFACTS"), Usage: "comma-separated globs for extra files to sign"},
		&cli.StringSliceFlag{Name: "file", Sources: planfile.Vars(planScopeSign, "file"), Usage: "exact file to sign in place (repeatable; sidecar stays next to the file)"},
		&cli.StringFlag{Name: flagManifest, Value: apprelease.DefaultReleaseFilesManifest, Sources: planfile.Vars(planScopeSign, flagManifest, "RELEASE_FILES_MANIFEST"), Usage: "release file manifest used by --manifest-section and --checksums-from-manifest"},
		&cli.StringFlag{Name: flagDistDir, Value: defaultDistDir, Sources: planfile.Vars(planScopeSign, flagDistDir), Usage: "dist directory used with --manifest"},
		&cli.StringSliceFlag{Name: "manifest-section", Sources: planfile.Vars(planScopeSign, "manifest-section"), Usage: "release file manifest section to sign exactly (repeatable): assets, checksums, sboms, evidence, provenance"},
		debugAllowSwapFlag(),
	}, signMethodFlags(planScopeSign)...)

	return &cli.Command{
		Name:  "sign",
		Usage: "detach-sign checksums.sha256, release artifacts, and attached artifacts. Method selectable via --method: gpg (default; .asc sidecar), sigstore (keyless cosign; .bundle sidecar), or kms (cosign + --key; .bundle sidecar).",
		Description: `Every flag except --debug-allow-swap (deliberately argv-only) may also be
fed from the $REUSABLE_CI_PLAN plan file under the "release sign" scope
(flag > plan > env > default).

EXAMPLES:
   # GPG-sign the checksums + every file in dist/ (key from $GPG_PRIVATE_KEY)
   reusable-ci release sign --method=gpg --release-artifacts-dir dist

   # Keyless (cosign) sign on a forge with an OIDC issuer
   reusable-ci release sign --method=sigstore --checksums-file checksums.sha256

   # KMS-backed cosign signing
   reusable-ci release sign --method=kms --key hashivault://transit/keys/release`,
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
			if method == domainrelease.SignMethodGPG {
				if err := requireNoSwapWithWarning(cmd); err != nil {
					return err
				}
			}

			return apprelease.SignArtifacts(ctx, signer, os.Stderr, apprelease.SignInput{
				ChecksumsFile:           cmd.String(flagChecksumsFile),
				SkipChecksumsFile:       cmd.Bool("no-checksums-file"),
				ReleaseArtifactsDir:     cmd.String(flagReleaseArtifactsDir),
				SkipReleaseArtifactsDir: cmd.Bool("no-release-artifacts-dir"),
				AttachArtifacts:         cmd.String(flagAttachArtifacts),
				Files:                   cmd.StringSlice("file"),
				AssemblyFile:            cmd.String(flagAssembly),
				ReleaseFilesManifest:    cmd.String(flagManifest),
				ReleaseFilesDistDir:     cmd.String(flagDistDir),
				ManifestSections:        cmd.StringSlice("manifest-section"),
				ChecksumsFromManifest:   cmd.Bool("checksums-from-manifest"),
			})
		},
	}
}

func downloadArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "download-artifacts",
		Usage: "download release artifacts from the explicit artifact-transfer plan",
		Description: cmdmeta.InCINote + `ARTIFACT_TRANSFER_PLAN_JSON comes from the release orchestrator's plan step.

EXAMPLE (as a workflow step):
   reusable-ci release download-artifacts --run-id 123456 --repository org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagArtifactTransferPlanJSON, Sources: cli.EnvVars("ARTIFACT_TRANSFER_PLAN_JSON"), Usage: "typed transfer plan JSON listing which CI artifacts to download"},
			&cli.StringFlag{Name: "run-id", Sources: cienv.RunID(), Usage: "CI run ID artifacts are downloaded from"},
			&cli.StringFlag{Name: flagRepository, Sources: cienv.Repository(), Usage: "\"owner/repo\" the run belongs to"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				dl, err := d.RequireRunArtifactDownloader()
				if err != nil {
					return err
				}

				return apprelease.DownloadArtifacts(ctx, dl, os.Stderr, apprelease.DownloadArtifactsInput{
					ArtifactTransferPlanJSON: cmd.String(flagArtifactTransferPlanJSON),
					RunID:                    cmd.String("run-id"),
					Repository:               cmd.String(flagRepository),
				})
			})
		},
	}
}

func checksumsCmd() *cli.Command {
	return &cli.Command{
		Name:  "checksums",
		Usage: "compute SHA256 over release artifacts, attached patterns, and SBOM layers",
		Description: `EXAMPLE:
   reusable-ci release checksums --release-artifacts-dir dist --output checksums.sha256`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagOutput, Sources: cli.EnvVars("OUTPUT_FILE"),
				Usage: "manifest path (default: checksums.sha256)"},
			&cli.StringFlag{Name: flagAssembly, Sources: cli.EnvVars("RELEASE_ASSEMBLY"), Usage: "release assembly manifest to checksum exactly"},
			&cli.StringFlag{Name: flagReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR"), Usage: "directory whose files are each hashed into the manifest"},
			&cli.StringFlag{Name: flagAttachArtifacts, Sources: cli.EnvVars("ATTACH_ARTIFACTS"),
				Usage: "comma-separated globs for additional files to checksum (paths kept verbatim)"},
			&cli.StringFlag{Name: flagSBOMDir, Sources: cli.EnvVars("SBOM_DIR"), Usage: "directory of SBOM layer files to include in the manifest"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			n, err := apprelease.Checksums(os.Stderr, apprelease.ChecksumsInput{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				OutputFile:          cmd.String(flagOutput),
				ReleaseArtifactsDir: cmd.String(flagReleaseArtifactsDir),
				AttachArtifacts:     cmd.String(flagAttachArtifacts),
				SBOMDir:             cmd.String(flagSBOMDir),
				AssemblyFile:        cmd.String(flagAssembly),
			})
			if err != nil {
				return err
			}

			if n == 0 {
				_, _ = fmt.Fprintln(os.Stderr, "No artifacts found to checksum.")
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
			Name:     flagVersion,
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
			Name:    flagSBOMDir,
			Sources: cli.EnvVars("SBOM_DIR"),
			Usage:   "directory holding the SBOM layers to bundle",
		},
		&cli.StringFlag{Name: flagAssembly, Sources: cli.EnvVars("RELEASE_ASSEMBLY"), Usage: "release assembly manifest whose SBOM inputs should be bundled"},
		debugAllowSwapFlag(),
	}, signMethodFlags("")...)

	return &cli.Command{
		Name:  "sbom-zip",
		Usage: "bundle all SBOM layers into <project>-<version>-sboms.zip; optionally sign via --sign + --method",
		Description: `EXAMPLE:
   reusable-ci release sbom-zip --project-name app --version 1.2.3 --sbom-dir sboms`,
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
				if method == domainrelease.SignMethodGPG {
					if err := requireNoSwapWithWarning(cmd); err != nil {
						return err
					}
				}

				signer = built
			}

			_, err := apprelease.CreateSBOMZip(ctx, signer, apprelease.SBOMZipInput{
				ProjectName:   cmd.String("project-name"),
				Version:       cmd.String(flagVersion),
				SBOMDir:       cmd.String(flagSBOMDir),
				SignArtifacts: cmd.Bool("sign"),
				AssemblyFile:  cmd.String(flagAssembly),
			}, os.Stderr)

			return err
		},
	}
}
