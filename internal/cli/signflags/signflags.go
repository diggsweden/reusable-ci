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
	return append([]cli.Flag{
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
	}, SigstoreEndpoints(EndpointOpts{
		PlanScope:    opts.PlanScope,
		ForbiddenFor: "--method=kms",
	})...)
}

// EndpointOpts carries the per-command nuance for the self-hosted Sigstore
// service flags.
type EndpointOpts struct {
	// PlanScope behaves as CosignOpts.PlanScope.
	PlanScope string
	// ForbiddenFor names the methods that reject these flags: "--method=kms"
	// for the cosign-only verbs, "--method=gpg/kms" where gpg is also a choice.
	ForbiddenFor string
}

// SigstoreEndpoints returns the flags that point signing at a Sigstore other
// than the public one: the issuing CA, the trust material used to verify the
// certificate, and the log it is published to. Shared by the cosign-only verbs
// (via Cosign) and by `release sign`, which also offers gpg and so cannot use
// Cosign wholesale.
func SigstoreEndpoints(opts EndpointOpts) []cli.Flag {
	forbidden := " Forbidden for " + opts.ForbiddenFor + "."

	return []cli.Flag{
		&cli.StringFlag{
			Name:    "fulcio-url",
			Sources: sources(opts.PlanScope, "fulcio-url", "SIGN_FULCIO_URL"),
			Usage:   "certificate authority for --method=sigstore (default: public Sigstore). Set this for a self-hosted Sigstore: --oidc-issuer alone does not redirect it, so the token is minted by your issuer and then presented to the public CA." + forbidden,
		},
		&cli.StringFlag{
			Name:    "trusted-root",
			Sources: sources(opts.PlanScope, "trusted-root", "SIGN_TRUSTED_ROOT"),
			Usage:   "trust material cosign verifies the new signature against, as produced by `cosign trusted-root create` (default: cosign's own). Required for a self-hosted CA: cosign verifies the certificate it was just issued and cannot learn a private root any other way." + forbidden,
		},
		&cli.StringFlag{
			Name:    "rekor-url",
			Sources: sources(opts.PlanScope, "rekor-url", "SIGN_REKOR_URL"),
			Usage:   "transparency log for --method=sigstore (default: public Sigstore). Where to publish, not whether: see REUSABLE_CI_COSIGN_TRANSPARENCY for that." + forbidden,
		},
	}
}

// Endpoints carries the self-hosted Sigstore service overrides, read through
// one helper so no call site can miss one.
type Endpoints struct {
	FulcioURL       string
	RekorURL        string
	TrustedRootPath string
}

// ReadEndpoints pulls the endpoint overrides from a parsed command.
func ReadEndpoints(cmd *cli.Command) Endpoints {
	return Endpoints{
		FulcioURL:       cmd.String("fulcio-url"),
		RekorURL:        cmd.String("rekor-url"),
		TrustedRootPath: cmd.String("trusted-root"),
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
