// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

var (
	errMetadataUnavailable  = errors.New("metadata unavailable")
	errSummaryAppendRefused = errors.New("summary append refused")
	errConsoleClosed        = errors.New("console closed")
)

// failingGitInfo answers each metadata method with the configured value, or
// fails the methods named in fail.
type failingGitInfo struct {
	fail   map[string]bool
	tagger git.TaggerInfo
	msg    string
	body   string
	commit git.CommitInfo
}

func (f failingGitInfo) TaggerInfo(context.Context, string) (git.TaggerInfo, error) {
	return f.tagger, f.err("tagger")
}

func (f failingGitInfo) TagMessage(context.Context, string) (string, error) {
	return f.msg, f.err("message")
}

func (f failingGitInfo) CatFileTag(context.Context, string) (string, error) {
	return f.body, f.err("body")
}

func (f failingGitInfo) CommitInfo(context.Context, string) (git.CommitInfo, error) {
	return f.commit, f.err("commit")
}

func (f failingGitInfo) err(method string) error {
	if f.fail[method] {
		return errMetadataUnavailable
	}

	return nil
}

// TestPrerequisites_MetadataFailuresDegradeSectionBySection fails each Git
// metadata method alone and all together. Each failure has its own safe
// placeholder (N/A, No message, Unavailable, an omitted commit section),
// neighbouring fields keep their values, the sections after it still render,
// and the report never claims a verified signature: presence stays
// "Unconfirmed" in the results table. The metadata is attacker-authored, so
// the working values carry Markdown and HTML that must arrive as literal text.
func TestPrerequisites_MetadataFailuresDegradeSectionBySection(t *testing.T) {
	t.Parallel()

	working := failingGitInfo{
		tagger: git.TaggerInfo{Tagger: "Eve <img src=x>", Date: "[today](https://phish.example)"},
		msg:    "**Approved** by security\nsecond line",
		body:   "object abc\n-----BEGIN SSH SIGNATURE-----\n",
		commit: git.CommitInfo{Author: "Mallory |x|", Date: "2026-05-09", Message: "<script>alert(1)</script>", Body: "tree abc\n"},
	}

	tests := map[string]struct {
		fail    []string
		want    []string
		without []string
	}{
		"nothing fails": {
			want: []string{
				"- **Tagger:** Eve &#60;img src=x&#62;\n", "- **Tag Date:** &#91;today&#93;(https&#58;//phish.example)\n",
				"- **Tag Signature:** SSH signed\n", "- **Tag Message:** &#42;&#42;Approved&#42;&#42; by security\n",
				"## 📦 Tagged Commit\n", "- **Author:** Mallory &#124;x&#124;\n", "- **Message:** &#60;script&#62;alert(1)&#60;/script&#62;\n",
			},
			without: []string{"<img", "<script>", "[today]", "second line"},
		},
		"tagger fails": {
			fail: []string{"tagger"},
			want: []string{"- **Tagger:** N/A\n", "- **Tag Date:** N/A\n", "- **Tag Signature:** SSH signed\n", "- **Tag Message:** &#42;&#42;Approved"},
		},
		"message fails": {
			fail: []string{"message"},
			want: []string{"- **Tagger:** Eve", "- **Tag Message:** No message\n"},
		},
		"tag body fails": {
			fail: []string{"body"},
			want: []string{"- **Tag Signature:** Unavailable\n", "- **Tag Message:** &#42;&#42;Approved"},
		},
		"commit fails": {
			fail:    []string{"commit"},
			want:    []string{"- **Tag Message:** &#42;&#42;Approved"},
			without: []string{"## 📦 Tagged Commit"},
		},
		"everything fails": {
			fail: []string{"tagger", "message", "body", "commit"},
			want: []string{
				"- **Tagger:** N/A\n", "- **Tag Date:** N/A\n", "- **Tag Signature:** Unavailable\n", "- **Tag Message:** No message\n",
			},
			without: []string{"## 📦 Tagged Commit"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gitr := working
			gitr.fail = map[string]bool{}

			for _, method := range tc.fail {
				gitr.fail[method] = true
			}

			sink := &fakeSummarySink{}
			require.NoError(t, appsummary.Prerequisites(t.Context(), sink, gitr, appsummary.PrerequisitesSummaryInput{
				TagName: "v1.0.0", CommitSHA: "abcdef0123", RefType: provider.RefTypeTag, HasReleaseToken: true,
				JobStatus: domainsummary.ResultFailure, Now: fixedNow(),
			}))

			body := sink.buf.String()
			for _, want := range append(tc.want,
				"## ⚙️ Configuration\n", "## 🔑 Required Secrets Status\n", "### ✗ Prerequisites validation failed\n",
				"| Tag Signature | Unconfirmed | Signature presence is not verification; see the prerequisite job result |\n",
				"*Generated at: 2026-05-10 14:30:00 UTC*",
			) {
				require.Contains(t, body, want)
			}

			for _, absent := range tc.without {
				require.NotContains(t, body, absent)
			}

			require.NotContains(t, body, "| Tag Signature | ✓")
		})
	}
}

// TestSnapshotReleaseSummary_BannerIsBestEffortAndSummaryIsNot decides the
// auxiliary writer policy: the console banner is a convenience, so a failing
// writer still produces the summary and success, while a refused summary is
// returned and the success line is not printed.
func TestSnapshotReleaseSummary_BannerIsBestEffortAndSummaryIsNot(t *testing.T) {
	t.Parallel()

	in := appsummary.SnapshotReleaseSummaryInput{ProjectType: projecttype.Maven, ReleaseRef: "main", ReleaseSHA: "abcdef0123", Now: fixedNow()}

	sink := &fakeSummarySink{}
	require.NoError(t, appsummary.SnapshotReleaseSummary(t.Context(), sink, failingBannerWriter{}, in))
	require.Contains(t, sink.buf.String(), "# Dev Release Summary\n")

	var banner bytes.Buffer

	err := appsummary.SnapshotReleaseSummary(t.Context(), refusingSummarySink{}, &banner, in)
	require.ErrorIs(t, err, errSummaryAppendRefused)
	require.Contains(t, banner.String(), "Generating Dev Release Summary")
	require.NotContains(t, banner.String(), "generated successfully")
}

type failingBannerWriter struct{}

func (failingBannerWriter) Write([]byte) (int, error) { return 0, errConsoleClosed }

type refusingSummarySink struct{}

func (refusingSummarySink) Append(context.Context, string) error { return errSummaryAppendRefused }

// TestPrerequisites_TagNameCannotCloseItsCodeSpan: git allows a backtick in a
// tag name, which ended the hand-written code span and let the rest of the
// name render as Markdown.
func TestPrerequisites_TagNameCannotCloseItsCodeSpan(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	require.NoError(t, appsummary.Prerequisites(t.Context(), sink, nil, appsummary.PrerequisitesSummaryInput{
		TagName: "v1`[x](https://phish.example)", RefType: provider.RefTypeTag, JobStatus: domainsummary.ResultFailure, Now: fixedNow(),
	}))

	line, _, _ := strings.Cut(strings.SplitN(sink.buf.String(), "- **Tag:** ", 2)[1], "\n")
	require.Equal(t, domainsummary.InlineCode("v1`[x](https://phish.example)"), line)
	require.NotContains(t, line, "`[x]")
}
