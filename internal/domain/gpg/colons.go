// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gpg holds pure GPG-related parsers and helpers.
//
// adapter/gpg shells out to the real `gpg` binary; the result is text
// that this package parses into typed values. No I/O, no subprocess.
package gpg

import (
	"strings"
)

// Metadata is the per-key info the import flow emits as outputs.
type Metadata struct {
	Fingerprint string // primary-key fingerprint, uppercase hex, no spaces
	KeyID       string // long key ID, 16 hex chars (= last 16 of the fingerprint)
	Name        string // primary-UID name
	Email       string // primary-UID email
}

// ParseKeygrips returns every `grp:` line's keygrip from
// `gpg --with-colons --with-keygrip` output, in source order.
// Empty slice if none match. The colons format is documented at
// https://github.com/gpg/gnupg/blob/master/doc/DETAILS.
func ParseKeygrips(text string) []string {
	var out []string

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "grp:") {
			if g := colonField(line, 10); g != "" {
				out = append(out, g)
			}
		}
	}

	return out
}

// colonField returns the n-th colon-separated field (1-indexed). Returns ""
// when n is out of range.
func colonField(line string, n int) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	parts := strings.Split(line, ":")
	if n < 1 || n > len(parts) {
		return ""
	}

	return parts[n-1]
}
