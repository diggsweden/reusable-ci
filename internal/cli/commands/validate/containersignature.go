// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// containerSignatureCmd wires `reusable-ci validate container-
// signature <image>@<digest>`.
//
// Distinct from `validate tag signature` (git tag) and `validate
// artifact-signature` (release file). This subcommand verifies a
// signature stored in the OCI registry next to the image. The
// signature is read from the registry — no sidecar files on disk.
//
// --method is required. For image signing the signature lives in
// the registry, not on disk, so there's no sidecar layout to
// auto-detect; the operator either knows the method (from the
// repo's artifacts.yml sign.method) or supplies it explicitly.
func containerSignatureCmd() *cli.Command {
	return &cli.Command{
		Name:      "container-signature",
		Usage:     "verify a cosign signature on an OCI image (sigstore or kms). Reads from the registry — no local sidecar.",
		ArgsUsage: "<registry/image@sha256:...>",
		Description: `EXAMPLE:
   reusable-ci validate container-signature ghcr.io/org/app@sha256:abc... --method sigstore \
     --cert-identity-regexp '^https://github\.com/org/' --cert-oidc-issuer https://token.actions.githubusercontent.com`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "method",
				Sources: cli.EnvVars("SIGN_METHOD"),
				Usage:   "verification method: sigstore or kms. gpg is rejected — OpenPGP cannot verify OCI signatures.",
			},
			&cli.StringFlag{
				Name:    "cert-identity-regexp",
				Sources: cli.EnvVars("CERT_IDENTITY_REGEXP"),
				Usage:   "regexp the Fulcio cert identity must match for --method=sigstore (matched against the full identity URL; example: ^https://github\\.com/<owner>/<repo>/)",
			},
			&cli.StringFlag{
				Name:    "cert-oidc-issuer",
				Sources: cli.EnvVars("CERT_OIDC_ISSUER"),
				Usage:   "OIDC issuer URL the Fulcio cert must claim for --method=sigstore",
			},
			&cli.StringFlag{
				Name:    "key",
				Sources: cli.EnvVars("SIGN_KEY"),
				Usage:   "cosign --key reference for --method=kms verification: KMS URI or local pubkey file path",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			image := cmd.Args().First()
			if image == "" {
				return fmt.Errorf("validate container-signature: image reference is required (registry/image@sha256:...): %w", errs.ErrMissingInput)
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			keyRef := cmd.String("key")
			identityRegexp := cmd.String("cert-identity-regexp")
			oidcIssuer := cmd.String("cert-oidc-issuer")

			// Default the keyless verification identity from the detected
			// forge unless this is a KMS verification (a non-empty --key).
			if keyRef == "" && method != domainrelease.SignMethodKMS {
				identityRegexp, oidcIssuer = keylessVerifyIdentity(identityRegexp, oidcIssuer)
			}

			return appcontainer.VerifyImage(ctx, cosign.New(), os.Stderr, appcontainer.VerifyImageInput{
				Image:              image,
				Method:             method,
				CertIdentityRegexp: identityRegexp,
				CertOIDCIssuer:     oidcIssuer,
				KeyRef:             keyRef,
			})
		},
	}
}
