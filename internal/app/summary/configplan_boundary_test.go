// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

func prerequisitesConfigPlan(t *testing.T) pipeline.ConfigPlan {
	t.Helper()

	cfg := config.Config{Artifacts: []config.Artifact{
		{Name: "lib", ProjectType: projecttype.Maven, BuildType: config.BuildTypeLibrary, PublishTo: []config.PublishTarget{config.PublishMavenCentral}},
		{Name: "pkg", ProjectType: projecttype.NPM, BuildType: config.BuildTypeApplication, PublishTo: []config.PublishTarget{config.PublishForgePackages}},
	}}
	require.NoError(t, config.Derive(&cfg))
	plan := pipeline.NewConfigPlan(&cfg)
	require.NoError(t, pipeline.ValidateConfigPlan(plan))

	return plan
}

func prerequisitesPlanJSON(t *testing.T, plan pipeline.ConfigPlan) string {
	t.Helper()

	body, err := json.Marshal(plan)
	require.NoError(t, err)

	return string(body)
}

func TestPrerequisites_ConfigPlanValidationPrecedesGitAndSummary(t *testing.T) {
	t.Parallel()

	for _, seed := range []string{"", "existing summary\n"} {
		for _, tc := range []struct {
			name, reason string
			change       func(*pipeline.ConfigPlan)
		}{
			{name: "producer"},
			{name: "later artifact", reason: "config-plan artifacts.all[1] has an empty or duplicate name", change: func(p *pipeline.ConfigPlan) { p.Artifacts.All[1].Name = "lib" }},
			{name: "publish projection", reason: "config-plan artifacts.forge_packages disagrees with artifacts.all", change: func(p *pipeline.ConfigPlan) { p.Artifacts.ForgePackages = nil }},
			{name: "authorization gate", reason: "config-plan any_require_authorization disagrees with artifacts.all", change: func(p *pipeline.ConfigPlan) { p.AnyRequireAuthorization = true }},
		} {
			t.Run(tc.name+"/"+seed, func(t *testing.T) {
				t.Parallel()

				plan := prerequisitesConfigPlan(t)
				if tc.change != nil {
					tc.change(&plan)
				}

				var summary strings.Builder
				summary.WriteString(seed)

				attempts := 0
				sink := appendFailureSink(func(_ context.Context, body string) error {
					attempts++

					summary.WriteString(body)

					return nil
				})
				gitr := &fakeGitInfo{}

				err := appsummary.Prerequisites(t.Context(), sink, gitr, appsummary.PrerequisitesSummaryInput{
					ConfigPlanJSON: prerequisitesPlanJSON(t, plan), TagName: "v1.2.3", CommitSHA: "commit-literal", RefType: provider.RefTypeTag, Now: fixedNow(),
				})
				if tc.change != nil {
					require.ErrorIs(t, err, errs.ErrInvalidConfig)
					require.Contains(t, err.Error(), tc.reason)
					require.Empty(t, gitr.calls)
					require.Zero(t, attempts)
					require.Equal(t, seed, summary.String())

					return
				}

				require.NoError(t, err)
				require.Equal(t, []string{"tagger v1.2.3", "message v1.2.3", "body v1.2.3", "commit commit-literal"}, gitr.calls)
				require.Equal(t, 1, attempts)
				require.Contains(t, summary.String(), "| **Project Types** | maven,npm |\n")
				require.Contains(t, summary.String(), "| **Build Types** | application,library |\n")
				require.Contains(t, summary.String(), "| MAVEN_CENTRAL_USERNAME | Maven Central auth |")
				require.Contains(t, summary.String(), "| Forge Packages |")
			})
		}
	}
}

func TestPrerequisites_ConfigPlanOptionalAndExplicitOverrides(t *testing.T) {
	t.Parallel()
	plan := prerequisitesConfigPlan(t)
	invalid := prerequisitesConfigPlan(t)

	invalid.Artifacts.NPM = nil
	for _, tc := range []struct {
		raw string
		bad bool
	}{
		{raw: ""}, {raw: " \n"}, {raw: prerequisitesPlanJSON(t, plan)},
		{raw: prerequisitesPlanJSON(t, invalid), bad: true},
	} {
		var summary strings.Builder

		attempts := 0
		sink := appendFailureSink(func(_ context.Context, body string) error {
			attempts++

			summary.WriteString(body)

			return nil
		})

		err := appsummary.Prerequisites(t.Context(), sink, nil, appsummary.PrerequisitesSummaryInput{
			ConfigPlanJSON: tc.raw, ProjectTypes: "python", BuildTypes: "explicit-build", PublishTo: "npmjs", Now: fixedNow(),
		})
		if tc.bad {
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), "config-plan artifacts.npm disagrees with artifacts.all")
			require.Zero(t, attempts)
			require.Empty(t, summary.String())

			continue
		}

		require.NoError(t, err)
		require.Equal(t, 1, attempts)
		require.Contains(t, summary.String(), "| **Project Types** | python |\n")
		require.Contains(t, summary.String(), "| **Build Types** | explicit-build |\n")
		require.Contains(t, summary.String(), "| NPM_TOKEN |")
		require.NotContains(t, summary.String(), "MAVEN_CENTRAL_USERNAME")
	}
}
