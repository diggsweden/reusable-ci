// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKeylessRunnerBoundary_DoesNotSelectTheAPITarget(t *testing.T) {
	for _, runner := range []string{"github", "gitlab", "local"} {
		t.Run(runner, func(t *testing.T) {
			testenv.New(t)
			t.Setenv("REUSABLE_CI_PROVIDER", "forgejo")
			t.Setenv("FORGEJO_SERVER_URL", "https://target.invalid")
			t.Setenv("FORGEJO_REPOSITORY", "target/repo")

			wantRegexp, wantIssuer := "", ""

			switch runner {
			case "github":
				t.Setenv("GITHUB_ACTIONS", "true")
				t.Setenv("GITHUB_SERVER_URL", "https://github.com")
				t.Setenv("GITHUB_REPOSITORY", "owner/repo")

				wantRegexp = `^https://github\.com/owner/repo/`
				wantIssuer = "https://token.actions.githubusercontent.com"
			case "gitlab":
				t.Setenv("GITLAB_CI", "true")
				t.Setenv("CI_SERVER_URL", "https://gitlab.invalid")
				t.Setenv("CI_PROJECT_URL", "https://gitlab.invalid/owner/repo")

				wantRegexp = `^https://gitlab\.invalid/owner/repo/`
				wantIssuer = "https://gitlab.invalid"
			}

			gotRegexp, gotIssuer := keylessVerifyIdentity("", "")
			require.Equal(t, wantRegexp, gotRegexp)
			require.Equal(t, wantIssuer, gotIssuer)
			require.Equal(t, provider.ForgeForgejo, platform.Detect())

			gotRegexp, gotIssuer = keylessVerifyIdentity("explicit-regexp", "")
			require.Equal(t, "explicit-regexp", gotRegexp)
			require.Equal(t, wantIssuer, gotIssuer)
		})
	}
}
