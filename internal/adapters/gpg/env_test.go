// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
)

// TestIsolatedEnv_KeepsGNUPGHOMEAndDropsSecrets pins both halves of the
// contract: the subprocess still finds its keyring, and none of the four
// secret-bearing variables travels with it.
func TestIsolatedEnv_KeepsGNUPGHOMEAndDropsSecrets(t *testing.T) {
	t.Setenv("GNUPGHOME", "/tmp/gnupg-test")
	t.Setenv("GPG_PRIVATE_KEY", "secret-key")
	t.Setenv("GPG_PASSPHRASE", "secret-passphrase")
	t.Setenv("GPG_SIGNING_KEY", "legacy-secret-key")
	t.Setenv("GPG_SIGNING_PASSWORD", "legacy-secret-passphrase")

	env := strings.Join(gpg.IsolatedEnv(), "\n")
	if !strings.Contains(env, "GNUPGHOME=/tmp/gnupg-test") {
		t.Fatalf("isolated env did not keep GNUPGHOME: %q", env)
	}

	for _, forbidden := range []string{"GPG_PRIVATE_KEY=", "GPG_PASSPHRASE=", "GPG_SIGNING_KEY=", "GPG_SIGNING_PASSWORD="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("isolated env leaked %s in %q", forbidden, env)
		}
	}
}
