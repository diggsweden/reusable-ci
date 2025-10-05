// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// sprintf keeps the recording TB's methods to one line each.
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// recordingTB is the smallest TB that keeps a logged skip, a test skip, an
// error and a fatal apart. Logf, Skipf and Errorf used to share one slice, so
// a gate that skipped the whole test where it should only log, or errored
// where it should skip, looked the same as the right behaviour.
type recordingTB struct {
	logs   []string
	skips  []string
	errors []string
	fatal  string
}

func (r *recordingTB) Helper()        {}
func (r *recordingTB) Cleanup(func()) {}
func (r *recordingTB) Logf(format string, args ...any) {
	r.logs = append(r.logs, sprintf(format, args...))
}
func (r *recordingTB) Skipf(format string, args ...any) {
	r.skips = append(r.skips, sprintf(format, args...))
}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, sprintf(format, args...))
}
func (r *recordingTB) Fatalf(format string, args ...any) { r.fatal = sprintf(format, args...) }

// Every Need must be wired into Requires. Without this, a Need added but not
// handled would fall to the default case and only be noticed the first time a
// scenario asked for it — during a live run, against a real forge.
func TestRequires_HandlesEveryNeed(t *testing.T) {
	setCurrentContract(t, writeContract(t, composeFixtureBody(t)))
	t.Setenv("LAB_RUNNER_FORGES", "gitlab")
	t.Setenv("RC_LIVE_GITLAB_ENDPOINT", "gitlab")

	for _, need := range []Need{NeedsInRunner, NeedsFulcio} {
		tb := &recordingTB{}
		if !Requires(tb, provider.ForgeGitLab, need) || tb.fatal != "" || len(tb.logs) != 0 {
			t.Errorf("Need %s must be satisfied by the complete fixture: %+v", need, tb)
		}
	}
}

// Pin identities and uniqueness independently of both the production list and
// Requires; a duplicate cannot stand in for a missing Need. The contiguous-iota
// probe additionally catches a newly handled Need beyond this inventory.
func TestAllNeeds_ListsEveryNeed(t *testing.T) {
	setCurrentContract(t, writeContract(t, composeFixtureBody(t)))

	want := []Need{NeedsInRunner, NeedsFulcio}
	got := allNeeds()

	counts := make(map[Need]int)
	for _, need := range got {
		counts[need]++
	}

	if len(got) != len(want) {
		t.Errorf("allNeeds() = %v, want exactly %v", got, want)
	}

	for _, need := range want {
		if counts[need] != 1 {
			t.Errorf("allNeeds() lists %s %d times, want once", need, counts[need])
		}
	}

	past := Need(len(want))

	tb := &recordingTB{}
	Requires(tb, provider.ForgeGitLab, past)

	if tb.fatal == "" {
		t.Errorf("Need %d is handled by Requires but missing from allNeeds(): "+
			"append it, or TestRequires_HandlesEveryNeed silently stops covering it", int(past))
	}
}

// An unmet need must say which contract field decides it, so the reader is sent
// to the environment's description of itself rather than to a symptom.
func TestRequires_NamesTheContractField(t *testing.T) {
	setCurrentContract(t, writeContract(t, composeFixtureBody(t)))
	t.Setenv("LAB_RUNNER_FORGES", "")

	for _, tc := range []struct {
		need Need
		want string
	}{
		{NeedsInRunner, "LAB_RUNNER_FORGES"},
	} {
		tb := &recordingTB{}
		if Requires(tb, provider.ForgeGitLab, tc.need) {
			t.Errorf("Need %d was satisfied by an empty environment", int(tc.need))

			continue
		}

		if len(tb.logs) == 0 || !strings.Contains(tb.logs[0], tc.want) {
			t.Errorf("Need %d skipped without naming %s: %v", int(tc.need), tc.want, tb.logs)
		}
	}
}

func TestRequires_InRunnerNeedsSelectedEndpointWorkflowCapability(t *testing.T) {
	body := strings.ReplaceAll(composeFixtureBody(t), `, "workflow-runs"`, "")
	setCurrentContract(t, writeContract(t, body))
	t.Setenv("LAB_RUNNER_FORGES", "gitlab")

	tb := &recordingTB{}
	if Requires(tb, provider.ForgeGitLab, NeedsInRunner) {
		t.Fatal("in-runner scenario accepted endpoint without workflow-runs")
	}

	if len(tb.logs) == 0 || !strings.Contains(tb.logs[0], "workflow-runs") {
		t.Fatalf("missing workflow-runs skip diagnostic: %v", tb.logs)
	}
}

// TestRequires_EachOutcomeIsDistinct walks Requires and ForgesMeeting through
// every outcome with the recording TB: satisfied, an unmet need that only
// logs (a forge the suite cannot drive, a runner not deployed, no issuer
// mapping), several needs stopping at the first unmet one, and the empty
// narrowing that skips the scenario instead of passing with nothing covered.
// Logged skips never skip the test and never fail it.
func TestRequires_EachOutcomeIsDistinct(t *testing.T) {
	gitlab := string(provider.ForgeGitLab)
	full := composeFixtureBody(t)
	withoutGitLabIssuer := strings.Replace(full, `,
      {
        "endpoint": "gitlab",
        "oidc_issuer": "https://gitlab.compose.forgelab:8443"
      }`, "", 1)

	if withoutGitLabIssuer == full {
		t.Fatal("fixture: the gitlab issuer mapping was not removed")
	}

	for name, tc := range map[string]struct {
		body     string
		runners  string
		forge    provider.ForgeAPI
		needs    []Need
		want     bool
		wantLogs []string
	}{
		"every need met": {body: full, runners: gitlab, forge: provider.ForgeGitLab, needs: []Need{NeedsInRunner, NeedsFulcio}, want: true},
		"forge the suite cannot drive": {
			body: full, runners: "github,gitlab", forge: provider.ForgeGitHub, needs: []Need{NeedsInRunner},
			wantLogs: []string{"SKIP github: the in-runner tier does not drive this forge yet"},
		},
		"runner not deployed": {
			body: full, runners: "forgejo", forge: provider.ForgeGitLab, needs: []Need{NeedsInRunner, NeedsFulcio},
			wantLogs: []string{"SKIP gitlab: this environment deployed no runner for it (LAB_RUNNER_FORGES)"},
		},
		"no issuer mapping": {
			body: withoutGitLabIssuer, runners: gitlab, forge: provider.ForgeGitLab, needs: []Need{NeedsInRunner, NeedsFulcio},
			wantLogs: []string{`SKIP gitlab: contract fulcio has no issuer mapping for endpoint "gitlab"`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			setCurrentContract(t, writeContract(t, tc.body))
			t.Setenv("LAB_RUNNER_FORGES", tc.runners)
			t.Setenv("RC_LIVE_GITLAB_ENDPOINT", gitlab)

			tb := &recordingTB{}

			got := Requires(tb, tc.forge, tc.needs...)
			if got != tc.want || tb.fatal != "" || len(tb.skips) != 0 || len(tb.errors) != 0 || !slices.Equal(tb.logs, tc.wantLogs) {
				t.Errorf("Requires = %v, tb = %+v; want %v with logs %q only", got, tb, tc.want, tc.wantLogs)
			}
		})
	}

	t.Run("narrowing keeps only the forges that meet every need", func(t *testing.T) {
		setCurrentContract(t, writeContract(t, full))
		t.Setenv("LAB_RUNNER_FORGES", gitlab)
		t.Setenv("RC_LIVE_GITLAB_ENDPOINT", gitlab)

		tb := &recordingTB{}

		got := ForgesMeeting(tb, []provider.ForgeAPI{provider.ForgeGitHub, provider.ForgeGitLab}, NeedsInRunner)
		if !slices.Equal(got, []provider.ForgeAPI{provider.ForgeGitLab}) || len(tb.logs) != 1 || len(tb.skips) != 0 || tb.fatal != "" {
			t.Errorf("ForgesMeeting = %v, tb = %+v; want only gitlab with one logged skip", got, tb)
		}
	})

	t.Run("narrowing to nothing skips the scenario", func(t *testing.T) {
		setCurrentContract(t, writeContract(t, full))
		t.Setenv("LAB_RUNNER_FORGES", "")

		tb := &recordingTB{}

		got := ForgesMeeting(tb, []provider.ForgeAPI{provider.ForgeGitHub, provider.ForgeGitLab}, NeedsInRunner)
		if len(got) != 0 || len(tb.logs) != 2 || len(tb.skips) != 1 || !strings.Contains(tb.skips[0], "would cover nothing") || tb.fatal != "" || len(tb.errors) != 0 {
			t.Errorf("ForgesMeeting = %v, tb = %+v; want an empty result, two logged skips and one test skip", got, tb)
		}
	})
}
