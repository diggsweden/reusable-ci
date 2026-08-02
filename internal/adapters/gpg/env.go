// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg

import (
	"os"
	"strings"
)

var runtimeKeep = map[string]bool{ //nolint:gochecknoglobals // read-only allow-set.
	"PATH":        true,
	"HOME":        true,
	"TMPDIR":      true,
	"GNUPGHOME":   true,
	"GPG_TTY":     true,
	"LANG":        true,
	"LC_ALL":      true,
	"LC_CTYPE":    true,
	"LC_MESSAGES": true,
}

// IsolatedEnv keeps only non-secret runtime variables required by gpg. Private
// keys and passphrases are supplied through stdin by the adapter methods.
func IsolatedEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}

		if runtimeKeep[key] {
			out = append(out, kv)
		}
	}

	return out
}
