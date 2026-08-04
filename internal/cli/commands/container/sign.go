// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// signCmd wires `reusable-ci container sign <image>@<digest>`.
//
// Image signing differs from artifact signing in two ways:
//
//  1. The signature is stored in the OCI registry as a sibling tag,
//     not as a local sidecar file. There is no .bundle, .sig, or
//     .asc next to anything. Verification reads the signature from
//     the registry too.
//  2. method=gpg is rejected — OpenPGP signs blobs, not OCI manifest
//     digests. Container signing is sigstore or kms only.
//
// The image reference must be a digest reference (image@sha256:...).
// Mutable tags are rejected by the adapter — signing a tag would
// produce a signature for whatever the tag points at right now,
// which a future re-tag could invalidate without warning.
func signCmd() *cli.Command {
	return &cli.Command{
		Name:      subCmdSign,
		Usage:     "sign an OCI image with cosign (sigstore or kms). Registry-attached storage; signature lives next to the image, not on disk.",
		ArgsUsage: "<registry/image@sha256:...>",
		Description: `EXAMPLES:
   # Keyless signing (sigstore / OIDC), e.g. from CI
   reusable-ci container sign ghcr.io/org/app@sha256:abc... --method sigstore

   # KMS-backed signing
   reusable-ci container sign ghcr.io/org/app@sha256:abc... --method kms --key awskms:///alias/release`,
		Flags: append(
			signflags.Cosign(signflags.CosignOpts{MethodNote: cosignMethodNoteGPG}),
			&cli.BoolFlag{
				Name:    flagRecursive,
				Value:   true,
				Sources: cli.EnvVars("SIGN_RECURSIVE"),
				Usage:   "walk manifest-list children, signing each per-arch digest in addition to the list itself. Default true (production releases use multi-arch manifest lists).",
			},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			image := cmd.Args().First()
			if image == "" {
				return fmt.Errorf("container sign: image reference is required (registry/image@sha256:...): %w", errs.ErrMissingInput)
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			endpoints := signflags.ReadEndpoints(cmd)

			return appcontainer.SignImage(ctx, cosign.New(), os.Stderr, appcontainer.SignImageInput{
				Image:      image,
				Method:     method,
				Recursive:  cmd.Bool(flagRecursive),
				KeyRef:     cmd.String("key"),
				OIDCIssuer: cmd.String("oidc-issuer"),
				FulcioURL:  endpoints.FulcioURL,
				RekorURL:   endpoints.RekorURL,
			})
		},
	}
}
