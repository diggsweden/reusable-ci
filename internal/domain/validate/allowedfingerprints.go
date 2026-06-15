// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"strings"
)

// AllowedFingerprintSet is the in-memory set of 40-character hex GPG
// primary-key fingerprints the project trusts to sign a release tag.
// Membership is case-insensitive (GPG renders fingerprints uppercase;
// inputs are normalised). The set is assembled with Add from the keys in
// `.reusable-ci/allowed_gpg_keys.asc` (see openpgp.PrimaryFingerprints) —
// that keys file is the single source of both verification material and
// the authorised set. The SSH side uses OpenSSH's allowed_signers format
// natively via `git verify-tag -c gpg.ssh.allowedSignersFile=<path>`.
type AllowedFingerprintSet struct {
	// fingerprints stores normalised (uppercase, no spaces) entries.
	fingerprints map[string]struct{}
}

// NewAllowedFingerprintSet returns an empty set ready for Add. Used when
// the allowlist is assembled from key material (allowed_gpg_keys.asc)
// rather than parsed from a fingerprints file.
func NewAllowedFingerprintSet() AllowedFingerprintSet {
	return AllowedFingerprintSet{fingerprints: map[string]struct{}{}}
}

// Add inserts fp into the set after normalising it. A value that isn't a
// valid 40-hex fingerprint is ignored rather than stored — a malformed
// derived entry must never silently widen the allowlist. Reports whether
// the value was accepted.
func (s *AllowedFingerprintSet) Add(fp string) bool {
	if s.fingerprints == nil {
		s.fingerprints = map[string]struct{}{}
	}

	normalised := normaliseFingerprint(fp)
	if !isValidFingerprint(normalised) {
		return false
	}

	s.fingerprints[normalised] = struct{}{}

	return true
}

// Has reports whether fp (case-insensitive, with or without spaces) is
// in the set.
func (s AllowedFingerprintSet) Has(fp string) bool {
	if s.fingerprints == nil {
		return false
	}

	_, ok := s.fingerprints[normaliseFingerprint(fp)]

	return ok
}

// Len returns the number of unique fingerprints. Useful for summary
// rendering ("3 authorised GPG signers").
func (s AllowedFingerprintSet) Len() int { return len(s.fingerprints) }

// normaliseFingerprint strips whitespace and uppercases the hex.
// Idempotent. Accepts the various GPG renderings — "ABCD EFGH …",
// "abcd efgh …", or the canonical concatenated form — and produces
// the canonical 40-uppercase-hex form for comparison.
func normaliseFingerprint(fp string) string {
	var buf strings.Builder

	buf.Grow(40) //nolint:mnd // GPG primary-key fingerprint length, fixed by RFC 4880bis.

	for _, chr := range fp {
		switch {
		case chr >= '0' && chr <= '9', chr >= 'A' && chr <= 'F':
			buf.WriteRune(chr)
		case chr >= 'a' && chr <= 'f':
			buf.WriteRune(chr - ('a' - 'A'))
		case chr == ' ' || chr == '\t':
			// strip whitespace
		default:
			// Any other character makes the fingerprint malformed; the
			// caller's isValidFingerprint check will reject it.
			buf.WriteRune(chr)
		}
	}

	return buf.String()
}

// isValidFingerprint reports whether s is exactly 40 hex digits
// (uppercase — caller normalises). 40 chars is the OpenPGP primary-key
// fingerprint length (RFC 4880bis §5.5.2, SHA-1 of the public key
// packet). Short key IDs (8 or 16 hex) are NOT accepted — they collide
// trivially and have no place in an allowlist.
func isValidFingerprint(s string) bool {
	if len(s) != 40 { //nolint:mnd // OpenPGP primary-key fingerprint length.
		return false
	}

	for _, chr := range s {
		if (chr < '0' || chr > '9') && (chr < 'A' || chr > 'F') {
			return false
		}
	}

	return true
}
