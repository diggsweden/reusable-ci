// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package signflags holds the cosign signing-flag set shared by the signing
// commands so their flag names, env vars, and common wording cannot drift
// apart. Each caller supplies only its command-specific nuance.
package signflags

import (
	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
)

// CosignOpts carries the per-command nuance appended to the shared cosign
// signing-flag usage. The flag names, SIGN_* env sources, KMS/PKCS#11 key
// vocabulary, and sigstore-vs-kms "forbidden for" rules stay centralized;
// only the trailing note differs per command.
type CosignOpts struct {
	// MethodNote is appended to the --method usage — e.g. "Omit to generate
	// only." for `release provenance`, or the OCI/gpg caveat for `container sign`.
	MethodNote string
	// KeyNote is appended to the --key usage.
	KeyNote string
	// PlanScope, when non-empty, additionally resolves each flag from that
	// $REUSABLE_CI_PLAN plan-file scope (flag > plan > env > default).
	// Empty keeps the plain SIGN_* env chains.
	PlanScope string
}

// Cosign returns the cosign-only signing flags (--method / --key /
// --oidc-issuer) shared by the signing commands `container sign`,
// `container attest`, and `release provenance`. Verification commands
// (validate *-signature) are deliberately NOT users: they constrain a
// signer identity (--cert-identity-regexp), not an OIDC issuer to sign with.
func Cosign(opts CosignOpts) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "method",
			Sources: sources(opts.PlanScope, "method", "SIGN_METHOD"),
			Usage:   join("signing backend: sigstore (keyless cosign + OIDC) or kms (cosign + --key).", opts.MethodNote),
		},
		&cli.StringFlag{
			Name:    "key",
			Sources: sources(opts.PlanScope, "key", "SIGN_KEY"),
			Usage:   join("cosign --key for --method=kms: KMS/PKCS#11 URI (awskms://, gcpkms://, hashivault://, azurekms://, pkcs11:), env://VAR, or file path. Forbidden for --method=sigstore.", opts.KeyNote),
		},
		&cli.StringFlag{
			Name:    "oidc-issuer",
			Sources: sources(opts.PlanScope, "oidc-issuer", "SIGN_OIDC_ISSUER"),
			Usage:   "OIDC issuer URL for --method=sigstore (default: cosign auto-detect). Forbidden for --method=kms.",
		},
	}
}

// sources resolves a flag from the plan-file scope first when the calling
// verb is plan-scoped; an empty scope keeps the plain env chain.
func sources(planScope, key string, envNames ...string) cli.ValueSourceChain {
	if planScope == "" {
		return cli.EnvVars(envNames...)
	}

	return planfile.Vars(planScope, key, envNames...)
}

// join appends a command-specific note to a shared usage base, if present.
func join(base, note string) string {
	if note == "" {
		return base
	}

	return base + " " + note
}
