// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Transparency selects whether a cosign-signed artifact is published to
// a Sigstore transparency log (Rekor). It is the sibling of SignMethod:
// the method decides *how* the signature is made, the transparency
// decides *what the world learns about it*.
//
// A Rekor entry is a hashedrekord carrying the artifact's SHA-256, the
// signature, the public key, and an integration timestamp. It does NOT
// carry the artifact's contents or its filename. So the exposure is the
// existence and timing of a signing event, plus a fingerprint anyone who
// later obtains the artifact can use to confirm it is the one signed.
// The entry is permanent, public, and append-only — it cannot be undone.
//
// Two values cover the operator matrix:
//
//   - TransparencyPublic: publish to the public Sigstore Rekor instance.
//     The default, and the point of Sigstore: it makes a signing event
//     independently auditable and gives the signature a trusted
//     timestamp. Correct for anything published openly.
//
//   - TransparencyNone: make no log entry. The signature still verifies
//     against the public key indefinitely — this is ordinary PKI — but
//     it is not publicly auditable, and verifying it requires
//     --insecure-ignore-tlog. Correct for artifacts that are not
//     published (internal-only builds) and for test suites, which must
//     never write to a permanent public log.
//
// Why there is no "none" for keyless: see TransparencyNone's doc.
type Transparency string

const (
	// TransparencyPublic publishes to the public Sigstore Rekor log.
	TransparencyPublic Transparency = "public"

	// TransparencyNone makes no transparency-log entry.
	//
	// This is only coherent for method=kms. A keyless (Sigstore)
	// signature is made with a ~10-minute Fulcio certificate, so a
	// verifier reaching it later needs evidence the certificate was
	// valid *at signing time*. Sigstore offers exactly two sources of
	// that evidence: a Rekor inclusion proof, or an RFC3161 timestamp
	// from a Timestamp Authority (cosign's --use-signed-timestamps).
	//
	// reusable-ci configures no TSA anywhere, so for this codebase Rekor
	// is the only available source and keyless+none is unverifiable by
	// construction. That is a property of our configuration, not a law
	// of Sigstore: if a TSA is ever added, revisit SignConfig.Validate,
	// which rejects the combination today.
	TransparencyNone Transparency = "none"
)

// DefaultTransparency is what the cosign methods use when neither the
// CLI nor artifacts.yml specifies one. Public is the default because
// transparency is the reason to adopt Sigstore at all, and because a
// default that quietly withheld evidence would be the wrong way round:
// opting out of the public record should be a written choice.
const DefaultTransparency = TransparencyPublic

// ValidTransparencies lists every Transparency in canonical order. It is
// the single definition of the set: ParseTransparency accepts exactly
// these values and the generated artifacts.yml JSON Schema renders its
// sign.transparency enum from this slice.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidTransparencies = []Transparency{
	TransparencyPublic,
	TransparencyNone,
}

// ParseTransparency validates and returns a Transparency from raw input.
// Empty input is rejected; callers wanting a default should fall back to
// DefaultTransparency themselves, so "user typed nothing" and "user
// typed garbage" stay distinct in error messages.
func ParseTransparency(raw string) (Transparency, error) {
	if raw == "" {
		return "", fmt.Errorf("sign transparency is empty: %w", errs.ErrMissingInput)
	}

	if slices.Contains(ValidTransparencies, Transparency(raw)) {
		return Transparency(raw), nil
	}

	return "", fmt.Errorf(
		"sign transparency %q is not one of [%s]: %w",
		raw, joinTransparencies(ValidTransparencies), errs.ErrInvalidConfig,
	)
}

// joinTransparencies renders a list for error messages: "public, none".
func joinTransparencies(values []Transparency) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = string(value)
	}

	return strings.Join(parts, ", ")
}

// PublishesToLog reports whether this setting writes a transparency-log
// entry. It is the single predicate the rest of the system asks, so no
// caller compares against the string "public".
func (t Transparency) PublishesToLog() bool {
	return t != TransparencyNone
}
