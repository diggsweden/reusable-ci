// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"os"
	"testing"
	"time"
)

// TestTimeoutFor_UnsetKeepsTheCompiledDefault is the guard that this refactor
// changed no deadline: with nothing in the environment, every named timeout
// still resolves to the value that was previously a literal.
func TestTimeoutFor_UnsetKeepsTheCompiledDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		get  func() time.Duration
		want time.Duration
	}{
		{"API", apiTimeout, time.Minute},
		{"SCRATCH", scratchTimeout, 2 * time.Minute},
		{"CLI", cliTimeout, 3 * time.Minute},
		{"WORKFLOW", workflowTimeout, 5 * time.Minute},
		{"WORKFLOW_POLL", workflowPollTimeout, 4 * time.Minute},
		{"WORKFLOW_LOG", workflowLogTimeout, 3 * time.Minute},
		{"HTTP_SHORT", httpShortTimeout, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := timeoutEnvPrefix + tc.name
			t.Setenv(key, "") // Register restoration before establishing a genuinely unset fixture.

			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}

			if got := tc.get(); got != tc.want {
				t.Errorf("%s = %s, want %s (the value it had as a literal)", tc.name, got, tc.want)
			}
		})
	}
}

func TestTimeoutFor_EnvOverridesTheDefault(t *testing.T) {
	t.Setenv(timeoutEnvPrefix+"CLI", "9m")
	t.Setenv(timeoutEnvPrefix+"API", "")

	if got := cliTimeout(); got != 9*time.Minute {
		t.Errorf("cliTimeout() = %s, want 9m from the environment", got)
	}

	// One override must not move its neighbours.
	if got := apiTimeout(); got != time.Minute {
		t.Errorf("apiTimeout() = %s, want the default 1m; RC_LIVE_TIMEOUT_CLI leaked", got)
	}
}

// TestTimeoutFor_EmptyValueIsNotAnOverride: an unset variable and one exported
// as "" are the same intent — CI templates routinely pass empty strings.
func TestTimeoutFor_EmptyValueIsNotAnOverride(t *testing.T) {
	t.Setenv(timeoutEnvPrefix+"CLI", "")

	if got := cliTimeout(); got != 3*time.Minute {
		t.Errorf("cliTimeout() = %s, want the 3m default for an empty override", got)
	}
}

// TestTimeoutFor_RejectsAnUnusableOverride is the point of the panic: an
// operator who mistypes a duration is trying to change a deadline, and running
// on the old one while looking successful is the failure this replaces.
func TestTimeoutFor_RejectsAnUnusableOverride(t *testing.T) {
	for _, bad := range []string{"8min", "later", "0s", "-5m"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(timeoutEnvPrefix+"CLI", bad)

			defer func() {
				if recover() == nil {
					t.Errorf("cliTimeout() accepted %q instead of failing loudly", bad)
				}
			}()

			_ = cliTimeout()
		})
	}
}

// TestTimeoutFor_EachOverrideMovesOnlyItsOwnDeadline sets each variable in
// turn to a value no default has and requires exactly that deadline to take
// it. Only CLI was overridden before, so an accessor reading a neighbour's
// variable -- WORKFLOW_POLL reading WORKFLOW, say -- passed every test.
func TestTimeoutFor_EachOverrideMovesOnlyItsOwnDeadline(t *testing.T) {
	deadlines := []struct {
		name string
		get  func() time.Duration
		def  time.Duration
	}{
		{"API", apiTimeout, time.Minute},
		{"SCRATCH", scratchTimeout, 2 * time.Minute},
		{"CLI", cliTimeout, 3 * time.Minute},
		{"WORKFLOW", workflowTimeout, 5 * time.Minute},
		{"WORKFLOW_POLL", workflowPollTimeout, 4 * time.Minute},
		{"WORKFLOW_LOG", workflowLogTimeout, 3 * time.Minute},
		{"HTTP_SHORT", httpShortTimeout, 30 * time.Second},
	}

	for _, set := range deadlines {
		t.Run(set.name, func(t *testing.T) {
			for _, other := range deadlines {
				t.Setenv(timeoutEnvPrefix+other.name, "")
			}

			t.Setenv(timeoutEnvPrefix+set.name, "7h13m")

			for _, check := range deadlines {
				want := check.def
				if check.name == set.name {
					want = 7*time.Hour + 13*time.Minute
				}

				if got := check.get(); got != want {
					t.Errorf("with %s%s set, %s = %s, want %s", timeoutEnvPrefix, set.name, check.name, got, want)
				}
			}
		})
	}
}
