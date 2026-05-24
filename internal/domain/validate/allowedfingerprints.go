// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidFingerprintLine is returned by ParseAllowedFingerprints
// when a line is non-empty, non-comment, and not a 40-character hex
// GPG fingerprint. Callers can wrap on errors.Is to branch on the
// schema-violation case specifically.
//
//nolint:gochecknoglobals // sentinel error.
var ErrInvalidFingerprintLine = errors.New("not a 40-character hex GPG fingerprint")

// AllowedFingerprintSet is the parsed `.reusable-ci/allowed_gpg_fingerprints`
// file: every 40-character hex GPG key fingerprint that the project trusts
// to sign a release tag. Membership is case-insensitive (GPG renders
// fingerprints uppercase, the file accepts either).
//
// The SSH side uses OpenSSH's allowed_signers format natively via
// `git verify-tag -c gpg.ssh.allowedSignersFile=<path>` — there's no
// equivalent file for GPG in any standard, so this is the minimal format:
// one fingerprint per line, # comments, blank lines ignored.
type AllowedFingerprintSet struct {
	// fingerprints stores normalised (uppercase, no spaces) entries.
	fingerprints map[string]struct{}
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

// ParseAllowedFingerprints reads the body of an
// `.reusable-ci/allowed_gpg_fingerprints` file. Format:
//
//   - one 40-character hex fingerprint per non-comment, non-blank line
//   - leading/trailing whitespace tolerated
//   - internal whitespace tolerated ("ABCD EFGH …" — GPG renders
//     fingerprints with spaces; we accept the rendering verbatim)
//   - lines starting with `#` are comments
//   - blank lines are ignored
//
// Returns an error citing the line number on the first malformed entry.
// The whole file is rejected on first error — partial-trust is a
// security smell.
func ParseAllowedFingerprints(body []byte) (AllowedFingerprintSet, error) {
	set := AllowedFingerprintSet{fingerprints: map[string]struct{}{}}

	for lineNum, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		normalised := normaliseFingerprint(line)
		if !isValidFingerprint(normalised) {
			return AllowedFingerprintSet{}, fmt.Errorf(
				"allowed_gpg_fingerprints line %d: %q: %w",
				lineNum+1, line, ErrInvalidFingerprintLine)
		}

		set.fingerprints[normalised] = struct{}{}
	}

	return set, nil
}

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
