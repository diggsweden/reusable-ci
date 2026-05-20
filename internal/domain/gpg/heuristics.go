// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// AgentConfig is the gpg-agent.conf content that import-gpg-key writes
// when a passphrase is provided. Matches the upstream
// crazy-max/ghaction-import-gpg defaults so cached passphrases live for
// the duration of the job.
//
// The leading marker comment delineates this as reusable-ci's own write
// into a config file gpg owns — clig.dev §Configuration: when you modify
// configuration that isn't your program's, say so in the file so anyone
// (e.g. a developer who finds their ~/.gnupg changed) can see what wrote
// it and how to undo it.
const AgentConfig = `# Managed by reusable-ci (release gpg) — passphrase pre-seeding for CI signing.
# reusable-ci wrote this gpg-agent.conf; delete it to restore your own.
default-cache-ttl 21600
max-cache-ttl 31536000
allow-preset-passphrase
`

// IsArmored reports whether the input is an ASCII-armored PGP key
// (the "-----BEGIN PGP …-----" header). Same heuristic the upstream
// action uses: anything else is treated as base64.
func IsArmored(key string) bool {
	trimmed := strings.TrimLeft(key, "\n")

	return strings.HasPrefix(trimmed, "-----")
}

// DecodeKey returns the raw key bytes from either an armored input
// (passed through unchanged) or a base64-encoded armored input
// (decoded once).
func DecodeKey(key string) ([]byte, error) {
	if IsArmored(key) {
		return []byte(key), nil
	}
	// Tolerate whitespace in the base64 (some workflow callers wrap their
	// secrets across multiple lines).
	cleaned := strings.Map(func(r rune) rune { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}

		return r
	}, key)

	out, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("decode base64 key: %w: %w", err, errs.ErrMalformedInput)
	}

	return out, nil
}

// HexEncodePassphrase returns the uppercase-hex byte representation of
// the input. UTF-8-faithful: matches `od -An -tx1 | tr -d ' \n'
// | tr 'a-f' 'A-F'` exactly. Used as the PRESET_PASSPHRASE argument to
// gpg-connect-agent (sent via stdin so the value never appears in `ps`).
func HexEncodePassphrase(passphrase string) string {
	if passphrase == "" {
		return ""
	}

	const hex = "0123456789ABCDEF"

	out := make([]byte, 0, len(passphrase)*2)
	for i := range len(passphrase) {
		c := passphrase[i]
		out = append(out, hex[c>>4], hex[c&0x0F])
	}

	return string(out)
}
