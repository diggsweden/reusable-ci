// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"strings"
	"testing"

	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

func TestConfigPlanBoundary_DuplicateConsumedMembers(t *testing.T) {
	t.Parallel()

	raw := producedBoundaryConfigPlan(t)
	for _, tc := range []struct{ field, other, path string }{
		{`"version":1`, `"ver\u0073ion":2`, "version"},
		{`"any_require_authorization":true`, `"ANY_REQUIRE_AUTHORIZATION":false`, "any_require_authorization"},
		{`"require_authorization":true`, `"REQUIRE_AUTHORIZATION":false`, "artifacts.all[1].require_authorization"},
		{`"settings_path":"settings.xml"`, `"settings_path":"other.xml"`, "artifacts.all[1].maven.settings_path"},
		{`"requires_id_token":true`, `"requires_id_token":false`, "sign.requires_id_token"},
		{`"go":[]`, `"GO":null`, "artifacts.go"},
	} {
		require.Contains(t, raw, tc.field)

		for _, pair := range []struct{ name, members string }{
			{"equal", tc.field + "," + tc.field}, {"forward", tc.field + "," + tc.other}, {"reverse", tc.other + "," + tc.field},
		} {
			t.Run(tc.path+"/"+pair.name, func(t *testing.T) {
				assertDuplicatePlanRefusal(t, strings.Replace(raw, tc.field, pair.members, 1), "config-plan."+tc.path+" has duplicate consumed member")
			})
		}
	}
	// Duplicate diagnostics must precede semantic version/projection errors.
	assertDuplicatePlanRefusal(t, `{"version":999,"artifacts":{"maven":[],"MAVEN":[{}]}}`, "config-plan.artifacts.maven has duplicate consumed member")
}

func assertDuplicatePlanRefusal(t *testing.T, raw, reason string) {
	t.Helper()

	for _, seed := range []string{"", "existing"} {
		var events []string

		sink := &patternSink{Sink: fakeoutputsink.New(t), events: &events}
		writer := &patternWriter{events: &events}
		summary := &executionSummary{writer: writer, events: &events}

		if seed != "" {
			require.NoError(t, sink.Sink.Set(t.Context(), "canary", seed))
			_, err := writer.WriteString(seed)
			require.NoError(t, err)
		}

		before := sink.AllScalar()

		t.Run("release/"+seed, func(t *testing.T) {
			plan, err := appplan.Release(t.Context(), sink, summary, appplan.ReleaseInput{ConfigPlanJSON: raw, ReleaseSBOMs: "all", ReleaseSignArtifacts: true, RefName: "v1.2.3"})
			require.Empty(t, events)
			require.Equal(t, before, sink.AllScalar())
			require.Equal(t, seed, writer.String())
			require.Nil(t, plan)
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.ErrorContains(t, err, reason)
		})
		t.Run("snapshot/"+seed, func(t *testing.T) {
			plan, err := appplan.SnapshotRelease(t.Context(), sink, appplan.SnapshotReleaseInput{ConfigPlanJSON: raw, ProjectType: "maven", SBOMs: "none", PublishNPM: false})
			require.Empty(t, events)
			require.Equal(t, before, sink.AllScalar())
			require.Equal(t, seed, writer.String())
			require.Nil(t, plan)
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.ErrorContains(t, err, reason)
		})
	}
}
