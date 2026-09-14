// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runcontext_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTokenBoundary_TrimsOnlyAmbientLineEndings(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"", "\n", "\r", "\r\n", "\n\r\n"} {
		const secret = " \tfixture\ninterior "

		env := map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_TOKEN": secret + suffix}

		get := func(key string) string { return env[key] }
		for _, chain := range []runcontext.CredentialVar{runcontext.Token(), runcontext.ReleaseToken()} {
			got := chain.Resolve(get)
			require.Equal(t, secret, got.For("https://github.com/o/r"))
			require.Empty(t, got.For("https://other.invalid/o/r"))
		}

		require.Equal(t, secret+suffix, runcontext.OperatorCredential(secret+suffix).For("https://other.invalid"))

		env["RELEASE_TOKEN"] = "release-canary\r\n"

		require.Equal(t, "release-canary", runcontext.ReleaseToken().Resolve(get).For("https://other.invalid"))
	}
}
