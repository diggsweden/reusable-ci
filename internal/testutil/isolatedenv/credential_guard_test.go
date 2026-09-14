// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package isolatedenv_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedenv"
	"os"
	"testing"
)

func TestIsolate_CredentialSourcesAreClearedAndRestored(t *testing.T) {
	keys := append(runcontext.Token().Keys(), runcontext.ReleaseToken().Keys()...)

	keys = append(keys, "NPM_TOKEN", "SIGN_KEY", "SIGSTORE_ID_TOKEN", "GPG_SIGNING_PASSWORD", "COSIGN_PASSWORD")
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "synthetic-canary")
			t.Run("isolated", func(t *testing.T) {
				isolatedenv.Isolate(t)

				if os.Getenv(key) != "" {
					t.Errorf("credential source %s survived isolation", key)
				}
			})

			if os.Getenv(key) != "synthetic-canary" {
				t.Errorf("credential source %s was not restored", key)
			}
		})
	}
}
