// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg

import (
	"strings"
	"testing"
)

func TestIsolatedEnvDropsSecrets(t *testing.T) {
	t.Setenv("GNUPGHOME", "/tmp/gnupg-test")
	t.Setenv("GPG_PRIVATE_KEY", "secret-key")
	t.Setenv("GPG_PASSPHRASE", "secret-passphrase")
	t.Setenv("GPG_SIGNING_KEY", "legacy-secret-key")
	t.Setenv("GPG_SIGNING_PASSWORD", "legacy-secret-passphrase")

	env := strings.Join(IsolatedEnv(), "\n")
	if !strings.Contains(env, "GNUPGHOME=/tmp/gnupg-test") {
		t.Fatalf("isolated env did not keep GNUPGHOME: %q", env)
	}

	for _, forbidden := range []string{"GPG_PRIVATE_KEY=", "GPG_PASSPHRASE=", "GPG_SIGNING_KEY=", "GPG_SIGNING_PASSWORD="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("isolated env leaked %s in %q", forbidden, env)
		}
	}
}
