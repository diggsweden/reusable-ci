// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/stretchr/testify/require"
)

// The existing dedup test feeds a, a, b, b, c — already in order — so the sort
// that makes this transform deterministic is never exercised. Remove it and the
// test still passes, while two runs over the same report start producing
// different GitLab documents and every diff of a stored report becomes noise.
func TestTrivyToGitLabDep_ScrambledReferencesSortAndDeduplicate(t *testing.T) {
	t.Parallel()

	report := parseTrivy(t, `{
		"Results": [{"Target": "x", "Vulnerabilities": [{
			"VulnerabilityID": "CVE-1",
			"PrimaryURL": "https://m/",
			"References": ["https://z/", "https://a/", "https://z/", "https://m/", "https://b/", "https://a/"]
		}]}]
	}`)

	links := security.TrivyToGitLabDep(report, security.Options{}).Vulnerabilities[0].Links

	urls := make([]string, 0, len(links))
	for _, link := range links {
		urls = append(urls, link.URL)
	}

	require.Equal(t, "https://a/,https://b/,https://m/,https://z/", strings.Join(urls, ","),
		"references arrived scrambled and with duplicates; the output must be sorted and unique regardless")
}

// Two transforms of the same report must produce the same document. The sort is
// what guarantees it, and Go's map iteration is what would otherwise break it.
func TestTrivyToGitLabDep_IsStableAcrossRuns(t *testing.T) {
	t.Parallel()

	const body = `{
		"Results": [{"Target": "x", "Vulnerabilities": [
			{"VulnerabilityID": "CVE-2", "PrimaryURL": "https://z/", "References": ["https://q/", "https://c/"]},
			{"VulnerabilityID": "CVE-1", "PrimaryURL": "https://a/", "References": ["https://y/", "https://b/"]}
		]}]
	}`

	fixed := security.Options{Now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}

	first := security.TrivyToGitLabDep(parseTrivy(t, body), fixed)
	second := security.TrivyToGitLabDep(parseTrivy(t, body), fixed)

	require.Equal(t, first, second, "two transforms of one report differ; a stored report would diff against itself")
}

// The default scan timestamp had only ever been checked for being non-empty, so
// a wrong one — the zero time, or a local time formatted as if it were UTC —
// would pass. Both matter to a consumer: the format string carries no offset,
// so whatever is written is read back as UTC.
func TestTrivyToGitLabDep_DefaultScanTimeIsTheCurrentUTCTime(t *testing.T) {
	t.Parallel()

	// The bounds bracket the call, so this is deterministic rather than
	// timing-dependent: the stamp has to land inside a window this test
	// created. Truncated because the format keeps whole seconds only.
	//
	// One honest limit. The UTC half of the claim is only distinguishable on a
	// machine whose local zone is not UTC — on a UTC runner a local-time bug
	// produces the same string, and no in-window check can tell them apart.
	// The window itself holds everywhere, which is what catches a zero or
	// stale default.
	before := time.Now().UTC().Truncate(time.Second)
	report := security.TrivyToGitLabDep(parseTrivy(t, `{"Results": []}`), security.Options{})
	after := time.Now().UTC()

	const layout = "2006-01-02T15:04:05"

	for _, tc := range []struct{ name, stamp string }{
		{"StartTime", report.Scan.StartTime},
		{"EndTime", report.Scan.EndTime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := time.Parse(layout, tc.stamp)
			require.NoErrorf(t, err, "%s = %q, which is not the documented layout", tc.name, tc.stamp)

			require.Falsef(t, parsed.Before(before),
				"%s is %s, before this test started (%s); a zero or stale default would look like this",
				tc.name, tc.stamp, before.Format(layout))
			require.Falsef(t, parsed.After(after),
				"%s is %s, after this test finished (%s); a local time written as UTC would look like this",
				tc.name, tc.stamp, after.Format(layout))
		})
	}
}

// An explicit Now must be used verbatim. Without this, the bounds check above
// would be satisfied by a transform that ignored Options.Now entirely.
func TestTrivyToGitLabDep_AnExplicitNowIsUsedVerbatim(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	report := security.TrivyToGitLabDep(parseTrivy(t, `{"Results": []}`), security.Options{Now: fixed})

	require.Equal(t, "2021-03-04T05:06:07", report.Scan.StartTime)
	require.Equal(t, "2021-03-04T05:06:07", report.Scan.EndTime)
}
