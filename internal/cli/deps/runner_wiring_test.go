// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package deps_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func TestBuild_RunnerSinkPolicy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		target       provider.ForgeAPI
		marker       string
		actions      bool
		nativeOutput bool
		summaryFile  bool
		wantOutput   string
		wantSummary  string
	}{
		{"forgejo native file", provider.ForgeGitLab, "FORGEJO_ACTIONS", true, true, true, "FORGEJO_OUTPUT", "GITHUB_STEP_SUMMARY"},
		{"forgejo native log", provider.ForgeGitHub, "FORGEJO_ACTIONS", true, true, false, "FORGEJO_OUTPUT", "stdout"},
		{"gitea alias file", provider.ForgeLocal, "GITEA_ACTIONS", true, false, true, "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"},
		{"forgejo alias log", provider.ForgeForgejo, "FORGEJO_ACTIONS", true, false, false, "GITHUB_OUTPUT", "stdout"},
		{"github cross forge file", provider.ForgeForgejo, "", true, false, true, "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"},
		{"github no summary", provider.ForgeForgejo, "", true, false, false, "GITHUB_OUTPUT", ""},
		{"outside runner ignores files", provider.ForgeForgejo, "FORGEJO_ACTIONS", false, true, true, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testenv.New(t)

			paths := map[string]string{}
			for _, key := range []string{"FORGEJO_OUTPUT", "GITHUB_OUTPUT", "CI_OUTPUT", "GITHUB_STEP_SUMMARY", "CI_SUMMARY_FILE", "stdout"} {
				paths[key] = env.Path(key)
				require.NoError(t, os.WriteFile(paths[key], []byte(key+" canary\n"), 0o600))

				if key != "stdout" {
					t.Setenv(key, paths[key])
				}
			}

			if !tc.nativeOutput {
				t.Setenv("FORGEJO_OUTPUT", "")
			}

			if !tc.summaryFile {
				t.Setenv("GITHUB_STEP_SUMMARY", "")
			}

			t.Setenv("REUSABLE_CI_PROVIDER", string(tc.target))
			t.Setenv("GITHUB_ACTIONS", boolEnv(tc.actions))

			if tc.marker != "" {
				t.Setenv(tc.marker, "true")
			}
			// Target metadata must not select the runner's output dialect.
			t.Setenv("FORGEJO_SERVER_URL", "https://target.invalid")
			t.Setenv("FORGEJO_REPOSITORY", "target/repo")
			t.Setenv("CI_RESULTS_DIR", env.Path("results"))

			logFile, err := os.OpenFile(paths["stdout"], os.O_WRONLY|os.O_APPEND, 0o600)
			require.NoError(t, err)
			// Build captures os.Stdout for Forgejo's real summary fallback.
			// A regular owned file avoids pipes, goroutines, and subprocesses.
			previousStdout := os.Stdout
			os.Stdout = logFile //nolint:reassign // serial test of Build's actual stdout wiring.

			t.Cleanup(func() {
				os.Stdout = previousStdout //nolint:reassign // restore the process stream before closing the owned file.

				require.NoError(t, logFile.Close())
			})

			ctx := context.Background()
			built, err := deps.Build(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, built.Close(ctx)) })
			require.Equal(t, tc.target, built.Platform)
			require.Equal(t, tc.target, built.Provider.Name())
			require.NoError(t, built.OutputSink.Set(ctx, "version-no-v", "1.2.3"))
			require.NoError(t, built.OutputSink.SetBool(ctx, "published", false))
			require.NoError(t, built.SummarySink.Append(ctx, "## Build\n"))
			require.NoError(t, built.SummarySink.Append(ctx, "Complete\n"))
			require.NoError(t, built.Close(ctx))
			require.ErrorContains(t, built.OutputSink.Set(ctx, "late", "must-not-write"), "output sink is closed")
			require.ErrorContains(t, built.OutputSink.SetMultiline(ctx, "late", []string{"must-not-write"}), "output sink is closed")
			require.NoError(t, built.Close(ctx))

			for key, path := range paths {
				body, readErr := os.ReadFile(path)
				require.NoError(t, readErr)

				want := key + " canary\n"
				if key == tc.wantOutput {
					want += "version-no-v=1.2.3\npublished=false\n"
				}

				if key == tc.wantSummary {
					want += "## Build\nComplete\n"
				}

				require.Equal(t, want, string(body), key)
			}

			require.NoDirExists(t, env.Path("results"))
		})
	}
}
