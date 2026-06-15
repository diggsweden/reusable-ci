// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// artifactSignatureCmd wires `reusable-ci validate artifact-
// signature`. The consumer side of the three signing methods —
// auto-detects from the sidecar layout (.asc → gpg, .bundle →
// cosign) and dispatches to gpg / cosign-keyless / cosign-kms
// verify. For .bundle, the keyless-vs-kms split is decided by
// whether --cert-identity-regexp or --key was supplied.
//
// Distinct from `validate tag signature`, which verifies the git
// tag itself. This subcommand verifies a release artefact (the
// `.tgz` / `.jar` / `.crate`) against its sidecar signature file.
func artifactSignatureCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifact-signature",
		Usage: "verify a release artefact's signature (gpg .asc, or cosign .bundle for sigstore/kms); method auto-detected from sidecars unless --method is set",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "artifact",
				Required: true,
				Sources:  cli.EnvVars("ARTIFACT"),
				Usage:    "path to the artefact whose signature is verified (e.g. ./app.tgz)",
			},
			&cli.StringFlag{
				Name:    "signature",
				Sources: cli.EnvVars("SIGNATURE"),
				Usage:   "path to the signature sidecar (default: <artefact>.sig or <artefact>.asc, auto-detected)",
			},
			&cli.StringFlag{
				Name:    "method",
				Sources: cli.EnvVars("SIGN_METHOD"),
				Usage:   "force verification method (gpg|sigstore|kms); omit to auto-detect from sidecar files",
			},
			// GPG-method flags
			&cli.StringFlag{
				Name:    "public-key",
				Sources: cli.EnvVars("PUBLIC_KEY"),
				Usage:   "armored GPG public key for --method=gpg verification (PEM literal)",
			},
			&cli.StringFlag{
				Name:    "public-key-file",
				Sources: cli.EnvVars("PUBLIC_KEY_FILE"),
				Usage:   "path to an armored GPG public key file for --method=gpg (alternative to --public-key)",
			},
			// Sigstore-keyless flags (the Fulcio cert is embedded
			// in the v3 bundle — no separate --certificate file
			// to supply; just the identity constraints).
			&cli.StringFlag{
				Name:    "cert-identity-regexp",
				Sources: cli.EnvVars("CERT_IDENTITY_REGEXP"),
				Usage:   "regexp the Fulcio cert identity must match for --method=sigstore (matched against the full identity URL; example: ^https://github\\.com/<owner>/<repo>/)",
			},
			&cli.StringFlag{
				Name:    "cert-oidc-issuer",
				Sources: cli.EnvVars("CERT_OIDC_ISSUER"),
				Usage:   "OIDC issuer URL the Fulcio cert must claim for --method=sigstore (e.g. https://token.actions.githubusercontent.com)",
			},
			// KMS / file-key flag
			&cli.StringFlag{
				Name:    "key",
				Sources: cli.EnvVars("SIGN_KEY"),
				Usage:   "cosign --key reference for --method=kms verification: KMS URI, PKCS#11 URI, or local pubkey file path",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			in := appvalidate.ArtifactSignatureInput{
				Artefact:           cmd.String("artifact"),
				SignaturePath:      cmd.String("signature"),
				Method:             domainrelease.SignMethod(cmd.String("method")),
				CertIdentityRegexp: cmd.String("cert-identity-regexp"),
				CertOIDCIssuer:     cmd.String("cert-oidc-issuer"),
				KeyRef:             cmd.String("key"),
			}

			if raw := cmd.String("public-key"); raw != "" {
				in.PublicKey = []byte(raw)
			} else if path := cmd.String("public-key-file"); path != "" {
				body, err := os.ReadFile(path) //nolint:gosec // operator-supplied verification key path.
				if err != nil {
					return err
				}

				in.PublicKey = body
			}

			return appvalidate.VerifyArtifactSignature(ctx, cosign.New(), os.Stderr, in)
		},
	}
}
