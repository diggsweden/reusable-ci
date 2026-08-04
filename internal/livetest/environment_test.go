// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// sprintf keeps the recording TB's methods to one line each.
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// recordingTB is the smallest TB that can tell "logged a skip" from "called
// Fatalf", which is the only distinction these tests need.
type recordingTB struct {
	logs  []string
	fatal string
}

func (r *recordingTB) Helper()        {}
func (r *recordingTB) Cleanup(func()) {}
func (r *recordingTB) Logf(format string, args ...any) {
	r.logs = append(r.logs, sprintf(format, args...))
}
func (r *recordingTB) Skipf(format string, args ...any) {
	r.logs = append(r.logs, sprintf(format, args...))
}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.logs = append(r.logs, sprintf(format, args...))
}
func (r *recordingTB) Fatalf(format string, args ...any) { r.fatal = sprintf(format, args...) }

// Every Need must be wired into Requires. Without this, a Need added but not
// handled would fall to the default case and only be noticed the first time a
// scenario asked for it — during a live run, against a real forge.
func TestRequires_HandlesEveryNeed(t *testing.T) {
	for _, need := range allNeeds() {
		tb := &recordingTB{}
		Requires(tb, provider.PlatformGitLab, need)

		if tb.fatal != "" {
			t.Errorf("Need %d is not wired into Requires: %s", int(need), tb.fatal)
		}
	}
}

// An unmet need must say which contract field decides it, so the reader is sent
// to the environment's description of itself rather than to a symptom.
func TestRequires_NamesTheContractField(t *testing.T) {
	t.Setenv("LAB_RUNNER_FORGES", "")
	t.Setenv("LAB_FULCIO_URL", "")

	for _, tc := range []struct {
		need Need
		want string
	}{
		{NeedsInRunner, "LAB_RUNNER_FORGES"},
		{NeedsFulcio, "LAB_FULCIO_URL"},
	} {
		tb := &recordingTB{}
		if Requires(tb, provider.PlatformGitLab, tc.need) {
			t.Errorf("Need %d was satisfied by an empty environment", int(tc.need))

			continue
		}

		if len(tb.logs) == 0 || !strings.Contains(tb.logs[0], tc.want) {
			t.Errorf("Need %d skipped without naming %s: %v", int(tc.need), tc.want, tb.logs)
		}
	}
}
