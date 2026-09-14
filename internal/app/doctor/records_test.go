// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/doctor"
)

// Every other test in this package probes one check by name. That is the right
// shape for asking "does this rule fire", and the wrong shape for the thing
// doctor actually promises, which is a COMPLETE punch-list: run one command and
// see everything wrong with your setup.
//
// Name-probing cannot see a check that stopped being emitted. It cannot see one
// emitted twice, so the operator fixes the same thing again. It cannot see a
// check that quietly became a warning when it used to fail, which is the change
// that turns a blocked release into a shipped mistake. Each of those leaves
// every existing test green.
//
// So these compare the whole record: the ordered list of identities and
// severities, and the invariants that hold across every configuration.

// checkRecord is the part of a Check that is a contract. The message text is
// deliberately excluded — it is prose and rewording it should not fail a test —
// but whether a remediation EXISTS is contract, because that is what an
// operator acts on.
type checkRecord struct {
	Name           string
	Severity       doctor.Severity
	HasRemediation bool
}

func recordsOf(checks []doctor.Check) []checkRecord {
	out := make([]checkRecord, 0, len(checks))
	for _, check := range checks {
		out = append(out, checkRecord{check.Name, check.Severity, strings.TrimSpace(check.Remediation) != ""})
	}

	return out
}

func TestRun_EmitsTheCompleteOrderedRecord(t *testing.T) {
	t.Parallel()

	const keylessWorkflow = "on: push\njobs:\n  release:\n    permissions:\n      id-token: write\n    steps: []\n"

	for _, tc := range []struct {
		name      string
		artifacts string
		files     map[string]string
		want      []checkRecord
	}{
		{
			name:      "default GPG setup",
			artifacts: "artifacts:\n  - name: my-app\n    project-type: meta\n",
			want: []checkRecord{
				{"artifacts.yml present", doctor.SeverityOK, false},
				{"artifacts.yml parses + validates", doctor.SeverityOK, false},
				{"workflows pin reusable-ci to a tag", doctor.SeverityOK, false},
				{"sign block valid", doctor.SeverityOK, false},
				{"release-authorization allowlist (n/a)", doctor.SeverityOK, false},
				{"workflow id-token permission (n/a)", doctor.SeverityOK, false},
			},
		},
		{
			name:      "sigstore without the grant",
			artifacts: "sign:\n  method: sigstore\nartifacts:\n  - name: my-app\n    project-type: meta\n",
			want: []checkRecord{
				{"artifacts.yml present", doctor.SeverityOK, false},
				{"artifacts.yml parses + validates", doctor.SeverityOK, false},
				{"workflows pin reusable-ci to a tag", doctor.SeverityOK, false},
				{"sign block valid", doctor.SeverityOK, false},
				{"release-authorization allowlist (n/a)", doctor.SeverityOK, false},
				{"workflow id-token permission", doctor.SeverityFail, true},
			},
		},
		{
			name:      "sigstore with the grant",
			artifacts: "sign:\n  method: sigstore\nartifacts:\n  - name: my-app\n    project-type: meta\n",
			files:     map[string]string{".github/workflows/release.yml": keylessWorkflow},
			want: []checkRecord{
				{"artifacts.yml present", doctor.SeverityOK, false},
				{"artifacts.yml parses + validates", doctor.SeverityOK, false},
				{"workflows pin reusable-ci to a tag", doctor.SeverityOK, false},
				{"sign block valid", doctor.SeverityOK, false},
				{"release-authorization allowlist (n/a)", doctor.SeverityOK, false},
				{"workflow id-token permission", doctor.SeverityOK, false},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checks, err := doctor.Run(doctor.Input{Root: writeRepo(t, tc.artifacts, tc.files)})
			if err != nil {
				t.Fatal(err)
			}

			got := recordsOf(checks)
			if len(got) != len(tc.want) {
				t.Fatalf("emitted %d checks, want %d:\ngot  %v\nwant %v", len(got), len(tc.want), got, tc.want)
			}

			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("check %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// These hold whatever the configuration is, so they are asserted across every
// shape the other tests in this package build rather than case by case.
func TestRun_RecordInvariantsHoldForEveryConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		artifacts string
		files     map[string]string
	}{
		{"gpg default", "artifacts:\n  - name: a\n    project-type: meta\n", nil},
		{"sigstore, no grant", "sign:\n  method: sigstore\nartifacts:\n  - name: a\n    project-type: meta\n", nil},
		{"unparsable artifacts", "artifacts: [\n", nil},
		{
			"authorisation required, no allowlist",
			"artifacts:\n  - name: a\n    project-type: meta\n    release:\n      require-allowlisted-signer: true\n",
			nil,
		},
		{
			"floating reusable-ci ref",
			"artifacts:\n  - name: a\n    project-type: meta\n",
			map[string]string{".github/workflows/r.yml": "on: push\njobs:\n  a:\n    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@main\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checks, err := doctor.Run(doctor.Input{Root: writeRepo(t, tc.artifacts, tc.files)})
			if err != nil {
				t.Fatal(err)
			}

			if len(checks) == 0 {
				t.Fatal("doctor emitted no checks; an empty punch-list reads as a clean setup")
			}

			assertCheckRecordShape(t, checks)
			assertCheckNamesAreUnique(t, checks)

			assertDerivedCountsFollowTheSeverities(t, checks)
		})
	}
}

// assertCheckRecordShape holds the per-check invariants: a severity the
// renderers model, an actionable remediation exactly when something needs
// fixing, and no empty identity.
func assertCheckRecordShape(t *testing.T, checks []doctor.Check) {
	t.Helper()

	for i, check := range checks {
		switch check.Severity {
		case doctor.SeverityOK:
			if strings.TrimSpace(check.Remediation) != "" {
				t.Errorf("check %d (%s) is ok but carries remediation; nothing needs fixing", i, check.Name)
			}
		case doctor.SeverityWarn, doctor.SeverityFail:
			if strings.TrimSpace(check.Remediation) == "" {
				t.Errorf("check %d (%s) is %s with no remediation; doctor's promise is to say what to do next",
					i, check.Name, check.Severity)
			}
		default:
			t.Errorf("check %d (%s) has severity %q, which FormatText and ExitCode do not model",
				i, check.Name, check.Severity)
		}

		if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Message) == "" {
			t.Errorf("check %d has an empty name or message: %+v", i, check)
		}
	}
}

// assertCheckNamesAreUnique is the duplicate case: an operator handed the same
// item twice fixes the same thing twice.
func assertCheckNamesAreUnique(t *testing.T, checks []doctor.Check) {
	t.Helper()

	seen := map[string]int{}
	for _, check := range checks {
		seen[check.Name]++
	}

	for name, count := range seen {
		if count > 1 {
			t.Errorf("check %q emitted %d times; an operator would fix the same thing twice", name, count)
		}
	}
}

// assertDerivedCountsFollowTheSeverities pins that the failure count and exit
// code are derived from the checks rather than maintained alongside them.
func assertDerivedCountsFollowTheSeverities(t *testing.T, checks []doctor.Check) {
	t.Helper()

	wantFailures := 0

	for _, check := range checks {
		if check.Severity == doctor.SeverityFail {
			wantFailures++
		}
	}

	if got := doctor.CountFailures(checks); got != wantFailures {
		t.Errorf("CountFailures = %d, want %d", got, wantFailures)
	}

	if got := doctor.ExitCode(checks); (got != 0) != (wantFailures > 0) {
		t.Errorf("ExitCode = %d with %d failures; a failing punch-list must not exit 0", got, wantFailures)
	}
}

// The JSON report is a machine contract: something parses it. Field names and
// the derived Failures count are part of that contract, so they are compared
// against the checks rather than trusted.
func TestReport_JSONCarriesEveryCheckAndItsOwnFailureCount(t *testing.T) {
	t.Parallel()

	root := writeRepo(t, "sign:\n  method: sigstore\nartifacts:\n  - name: a\n    project-type: meta\n", nil)

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := doctor.FormatJSON(&out, doctor.Report{Checks: checks, Failures: doctor.CountFailures(checks)}); err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Checks []struct {
			Name        string `json:"name"`
			Severity    string `json:"severity"`
			Message     string `json:"message"`
			Remediation string `json:"remediation"`
		} `json:"checks"`
		Failures int `json:"failures"`
	}

	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
		t.Fatalf("the report must be parsable by whatever consumes it: %v\n%s", err, out.String())
	}

	if len(decoded.Checks) != len(checks) {
		t.Fatalf("report carries %d checks, doctor produced %d", len(decoded.Checks), len(checks))
	}

	for i, check := range checks {
		got := decoded.Checks[i]
		if got.Name != check.Name || got.Severity != string(check.Severity) || got.Message != check.Message {
			t.Errorf("check %d round-tripped as %+v, want %+v", i, got, check)
		}
	}

	if decoded.Failures != doctor.CountFailures(checks) {
		t.Errorf("report failures = %d, want %d", decoded.Failures, doctor.CountFailures(checks))
	}

	if decoded.Failures == 0 {
		t.Fatal("a sigstore repo with no id-token grant must report a failure, or this comparison is between two zeroes")
	}
}
