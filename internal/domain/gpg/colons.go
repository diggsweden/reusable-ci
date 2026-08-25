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

// AgentAck classifies a gpg-connect-agent transcript.
//
// This exists because gpg-connect-agent's EXIT STATUS carries no
// information: it exits 0 when the agent answers "ERR", and it exits 0
// even when it cannot reach an agent at all ("can't connect to the
// gpg-agent … No agent running"). Trusting the status meant a failed
// PRESET_PASSPHRASE looked like success, and the failure only surfaced
// much later as gpg falling back to pinentry — which in a container
// means "Inappropriate ioctl for device", pointing nowhere near the
// actual cause.
//
// The Assuan protocol answers each command with a line: "OK" (optionally
// followed by text) or "ERR <code> <description>". So a transcript is
// acknowledged only when it carries an OK and no ERR. detail is the line
// worth showing an operator: the ERR itself, or the first non-empty line
// when nothing acknowledged.
func AgentAck(transcript string) (ok bool, detail string) {
	var acked bool

	var first string

	for _, raw := range strings.Split(transcript, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if first == "" {
			first = line
		}

		if strings.HasPrefix(line, "ERR ") {
			return false, line
		}

		if line == "OK" || strings.HasPrefix(line, "OK ") {
			acked = true
		}
	}

	if acked {
		return true, ""
	}

	if first == "" {
		return false, "no response from gpg-agent"
	}

	return false, first
}
