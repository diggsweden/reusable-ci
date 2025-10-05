// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func TestCIFixtureBoundary_IsolatesAllAmbientAliases(t *testing.T) {
	for key, value := range map[string]string{"FORGEJO_ACTIONS": "true", "GITHUB_ACTIONS": "true", "GITLAB_CI": "true", "FORGEJO_WORKFLOW_REF": "ambient/workflow", "CI_JOB_URL": "https://ambient.invalid/job"} {
		t.Setenv(key, value)
	}

	for _, variable := range []runcontext.Var{runcontext.RefName(), runcontext.Ref(), runcontext.Repository(), runcontext.ServerURL(), runcontext.CheckoutRef()} {
		for _, key := range variable.Keys() {
			t.Setenv(key, "ambient-canary")
		}
	}

	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{
		{"empty", TestNonEmptyEnvSource_SkipsSetButEmpty}, {"first", TestNonEmptyEnvSource_FirstNonEmptyWins}, {"absent", TestNonEmptyEnvSource_AllUnsetIsAbsent}, {"server", TestServerURL_ForgeFallbacks}, {"checkout", TestCheckoutRef_ExplicitOverrideWinsOverCommit},
		{"builder", TestProvenanceBuilderID_CanonicalWorkflowRef}, {"invocation", TestProvenanceInvocationID_RunURL}, {"job", TestProvenanceBuilderID_FallsBackToJobURL},
	} {
		t.Run(test.name, test.fn)
	}
}

func TestEpochBoundary_OnlyRepresentableRFC3339(t *testing.T) {
	testenv.New(t)

	for _, tc := range []struct {
		seconds int64
		want    string
	}{
		{-62167219200, "0000-01-01T00:00:00Z"}, {253402300799, "9999-12-31T23:59:59Z"}, {-62167219201, ""}, {253402300800, ""}, {9223372036854775807, ""}, {-9223372036854775808, ""},
	} {
		t.Setenv("SOURCE_DATE_EPOCH", strconv.FormatInt(tc.seconds, 10))

		got, ok := cienv.SourceDateEpochRFC3339()
		require.Equal(t, tc.want != "", ok)
		require.Equal(t, tc.want, got)

		if ok {
			_, err := time.Parse(time.RFC3339, got)
			require.NoError(t, err)
		}
	}
}
