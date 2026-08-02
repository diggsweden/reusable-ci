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

// containerAttestationCmd wires `reusable-ci validate container-attestation
// <image>@<digest>`.
//
// The verify side of `container attest`: it re-checks a signed in-toto
// attestation (SLSA v1.0 provenance or an SBOM) attached to the image in the
// registry, against the signing identity. This is the trust-boundary
// re-verification forgejo-ci's promote/verify-base workflows do before moving a
// tag — closing the attest-without-verify gap. Sibling of container-signature
// (which verifies the image signature); both read from the registry, no sidecar.
func containerAttestationCmd() *cli.Command {
	return &cli.Command{
		Name:      "container-attestation",
		Usage:     "verify a signed in-toto attestation (slsaprovenance1 | cyclonedx | spdx) on an OCI image (sigstore or kms). Reads from the registry — no local sidecar.",
		ArgsUsage: "<registry/image@sha256:...>",
		Description: `EXAMPLE:
   reusable-ci validate container-attestation ghcr.io/org/app@sha256:abc... \
     --type slsaprovenance1 --method sigstore \
     --cert-identity-regexp '^https://github\.com/org/' --cert-oidc-issuer https://token.actions.githubusercontent.com`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "type",
				Sources: cli.EnvVars("PREDICATE_TYPE"),
				Usage:   "predicate type to verify: slsaprovenance1 (SLSA v1.0; the obsolete v0.2 'slsaprovenance' is rejected) | cyclonedx | spdx | <uri>",
			},
			&cli.StringFlag{
				Name:    flagMethod,
				Sources: cli.EnvVars("SIGN_METHOD"),
				Usage:   "verification method: sigstore or kms. gpg is rejected — OpenPGP cannot verify OCI attestations.",
			},
			&cli.StringFlag{
				Name:    flagCertIdentityRegexp,
				Sources: cli.EnvVars("CERT_IDENTITY_REGEXP"),
				Usage:   usageCertIdentityRegexp,
			},
			&cli.StringFlag{
				Name:    flagCertOIDCIssuer,
				Sources: cli.EnvVars("CERT_OIDC_ISSUER"),
				Usage:   "OIDC issuer URL the Fulcio cert must claim for --method=sigstore",
			},
			&cli.StringFlag{
				Name:    flagKey,
				Sources: cli.EnvVars("SIGN_KEY"),
				Usage:   "cosign --key reference for --method=kms verification: KMS URI or local pubkey file path",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			image := cmd.Args().First()
			if image == "" {
				return fmt.Errorf("validate container-attestation: image reference is required (registry/image@sha256:...): %w", errs.ErrMissingInput)
			}

			method, err := domainrelease.ParseSignMethod(cmd.String(flagMethod))
			if err != nil {
				return err
			}

			keyRef := cmd.String(flagKey)
			identityRegexp := cmd.String(flagCertIdentityRegexp)
			oidcIssuer := cmd.String(flagCertOIDCIssuer)

			// Default the keyless verification identity from the detected forge
			// unless this is a KMS verification (a non-empty --key).
			if keyRef == "" && method != domainrelease.SignMethodKMS {
				identityRegexp, oidcIssuer = keylessVerifyIdentity(identityRegexp, oidcIssuer)
			}

			return appcontainer.VerifyAttestation(ctx, cosign.New(), os.Stderr, appcontainer.VerifyAttestationInput{
				Image:              image,
				Method:             method,
				PredicateType:      cmd.String("type"),
				CertIdentityRegexp: identityRegexp,
				CertOIDCIssuer:     oidcIssuer,
				KeyRef:             keyRef,
			})
		},
	}
}
