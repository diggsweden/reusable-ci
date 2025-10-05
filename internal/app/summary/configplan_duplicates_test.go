// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

func TestPrerequisites_ConfigPlanDuplicateMembers(t *testing.T) {
	t.Parallel()

	const (
		artifact = `{"name":"lib","project_type":"maven","sboms":"none","require_authorization":true,"build_artifact_name":"lib-build-artifacts","build_sbom_artifact_name":"lib-build-sbom"}`
		raw      = `{"version":1,"artifacts":{"all":[` + artifact + `],"maven":[` + artifact + `],"npm":[]},"pipeline_sboms":"none","fallback_project_type":"maven","any_require_authorization":true,"sign":{"method":"gpg","imports_gpg_key":true},"git_signing":{"method":"gpg","imports_gpg_key":true},"future":{"version":1,"version":2}}`
	)

	cases := []struct{ name, raw, path string }{{"control", raw, ""}}
	for _, tc := range []struct{ first, second, path string }{
		{`"version":1`, `"ver\u0073ion":2`, "version"},
		{`"any_require_authorization":true`, `"any_require_authorization":false`, "any_require_authorization"},
		{`"npm":[]`, `"NPM":null`, "artifacts.npm"},
		{`"require_authorization":true`, `"REQUIRE_AUTHORIZATION":false`, "artifacts.all[0].require_authorization"},
	} {
		for _, pair := range []struct{ name, members string }{
			{"equal", tc.first + "," + tc.first}, {"forward", tc.first + "," + tc.second}, {"reverse", tc.second + "," + tc.first},
		} {
			cases = append(cases, struct{ name, raw, path string }{tc.path + "/" + pair.name, strings.Replace(raw, tc.first, pair.members, 1), tc.path})
		}
	}

	for _, tc := range cases {
		for _, seed := range []string{"", "existing\n"} {
			t.Run(tc.name+"/"+seed, func(t *testing.T) {
				t.Parallel()

				var summary strings.Builder
				summary.WriteString(seed)

				attempts := 0
				sink := appendFailureSink(func(_ context.Context, body string) error {
					attempts++

					summary.WriteString(body)

					return nil
				})
				git := &fakeGitInfo{}

				err := appsummary.Prerequisites(t.Context(), sink, git, appsummary.PrerequisitesSummaryInput{
					ConfigPlanJSON: tc.raw, ProjectTypes: "python", BuildTypes: "explicit", PublishTo: "npmjs",
					TagName: "v1.2.3", CommitSHA: "literal", RefType: provider.RefTypeTag, Now: fixedNow(),
				})
				if tc.path == "" {
					require.NoError(t, err)
					require.Len(t, git.calls, 4)
					require.Equal(t, 1, attempts)
					require.Contains(t, summary.String(), "| **Project Types** | python |")
					require.Contains(t, summary.String(), "| **Build Types** | explicit |")
					require.Contains(t, summary.String(), "| NPM_TOKEN |")

					return
				}

				require.Empty(t, git.calls)
				require.Zero(t, attempts)
				require.Equal(t, seed, summary.String())
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				require.ErrorContains(t, err, "config-plan."+tc.path+" has duplicate consumed member")
			})
		}
	}
}
