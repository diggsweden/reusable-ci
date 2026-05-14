// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package gpg holds pure GPG-related parsers and helpers.
//
// adapter/gpg shells out to the real `gpg` binary; the result is text
// that this package parses into typed values. No I/O, no subprocess.
package gpg

import (
	"strings"
)

// Metadata is the per-key info import emits as outputs.
type Metadata struct {
	Fingerprint string // primary-key fingerprint, uppercase hex, no spaces
	KeyID       string // long key ID, 16 hex chars (= last 16 of the fingerprint)
	Name        string // parsed from primary user UID
	Email       string // parsed from primary user UID
}

// ParseColonsOutput parses `gpg --batch --with-colons --list-secret-keys [<fpr>]`
// into a Metadata. The colons format is documented at:
//
//	https://github.com/gpg/gnupg/blob/master/doc/DETAILS
//
// Field map (1-indexed in the spec; 0-indexed here):
//
//	sec  field 5  = long keyID
//	fpr  field 10 = fingerprint
//	uid  field 10 = "Name (optional comment) <email@host>"
//
// Mirrors the bash parse_key_metadata in scripts/release/import-gpg-key.sh.
func ParseColonsOutput(text string) Metadata {
	var md Metadata
	var uid string

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "sec:"):
			if md.KeyID == "" {
				md.KeyID = field(line, 5)
			}
		case strings.HasPrefix(line, "fpr:"):
			if md.Fingerprint == "" {
				md.Fingerprint = field(line, 10)
			}
		case strings.HasPrefix(line, "uid:"):
			if uid == "" {
				uid = field(line, 10)
			}
		}
	}

	md.Name, md.Email = splitUID(uid)
	return md
}

// ParseFingerprint returns the first `fpr:` line's fingerprint from
// colons output, or "" when none. Used by the import flow to discover
// the fingerprint of a freshly-imported key without listing the entire
// keyring.
func ParseFingerprint(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "fpr:") {
			return field(line, 10)
		}
	}
	return ""
}

// ParseKeygrips returns every `grp:` line's keygrip from
// `gpg --with-colons --with-keygrip` output, in source order.
// Empty slice if none match.
func ParseKeygrips(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "grp:") {
			if g := field(line, 10); g != "" {
				out = append(out, g)
			}
		}
	}
	return out
}

// field returns the n-th colon-separated field (1-indexed). Returns ""
// when n is out of range.
func field(line string, n int) string {
	parts := strings.Split(line, ":")
	if n < 1 || n > len(parts) {
		return ""
	}
	return parts[n-1]
}

// splitUID separates a UID string of the form
//
//	Name Surname (optional comment) <email@host>
//
// into name + email. Strips trailing whitespace and any (comment) before
// the angle bracket. When no '<' is present, the whole input becomes
// the name and email is empty.
func splitUID(uid string) (name, email string) {
	if idx := strings.Index(uid, "<"); idx >= 0 {
		name = uid[:idx]
		emailPart := uid[idx+1:]
		if endIdx := strings.Index(emailPart, ">"); endIdx >= 0 {
			email = emailPart[:endIdx]
		} else {
			email = emailPart
		}
		// Drop "(comment)" before the bracket.
		if cIdx := strings.Index(name, " ("); cIdx >= 0 {
			name = name[:cIdx]
		}
		// Trim trailing whitespace.
		name = strings.TrimRight(name, " \t")
		return name, email
	}
	return uid, ""
}
