// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestRefType_TagSucceedsWithMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := appvalidate.RefType(&buf, appvalidate.RefTypeInput{
		RefType: provider.RefTypeTag, RefName: "v1.0.0", Ref: "refs/tags/v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Triggered by tag: v1.0.0") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRefType_BranchFailsWithGuidance(t *testing.T) {
	t.Parallel()

	err := appvalidate.RefType(&bytes.Buffer{}, appvalidate.RefTypeInput{
		RefType: provider.RefTypeBranch, RefName: "main", Ref: "refs/heads/main",
	})
	// A branch trigger is the operator running the wrong workflow, not a bad
	// flag: ErrValidation (exit 1), and the guidance below is the fix.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	for _, want := range []string{
		"release workflow must be triggered by pushing a tag",
		"Current trigger: branch",
		"git tag -s v1.0.0",
		"git push origin v1.0.0",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\nfull: %s", want, err.Error())
		}
	}
}

func TestRefType_EmptyTypeUsage(t *testing.T) {
	t.Parallel()

	err := appvalidate.RefType(&bytes.Buffer{}, appvalidate.RefTypeInput{})
	// The name is the claim: an empty ref type is a broken invocation.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v, want it to carry a usage line", err)
	}
}

func TestTagFormat_StableReleaseOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Valid semantic version tag",
		"Version: 1.0.0",
		"Stable release",
		"validation passed",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in output:\n%s", want, buf.String())
		}
	}
}

func TestTagFormat_StandardPrerelease(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0-rc.2"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Pre-release: rc.2") {
		t.Errorf("output = %s", buf.String())
	}

	if !strings.Contains(buf.String(), "follows convention") {
		t.Errorf("output should flag standard prerelease, got:\n%s", buf.String())
	}
}

func TestTagFormat_NonStandardPrerelease(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := appvalidate.TagFormat(&buf, appvalidate.TagFormatInput{Tag: "v1.0.0-custom.1"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Non-standard pre-release identifier") {
		t.Errorf("expected non-standard warning, got:\n%s", buf.String())
	}

	if !strings.Contains(buf.String(), "informational only") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestTagFormat_BadTagShowsHelp(t *testing.T) {
	t.Parallel()

	err := appvalidate.TagFormat(&bytes.Buffer{}, appvalidate.TagFormatInput{Tag: "1.0.0"})
	// A tag that is not semver is a domain-rule failure, not a bad flag.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	for _, want := range []string{"invalid tag format", "vMAJOR.MINOR.PATCH", "semver.org", "v1.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err missing %q: %s", want, err.Error())
		}
	}
}

func TestReleaseTagGuard_StableTagOutputsReleaseTag(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appvalidate.ReleaseTagGuard(context.Background(), sink, &out, appvalidate.ReleaseTagGuardInput{Tag: "refs/tags/v1.2.3"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("release-tag"); got != "v1.2.3" {
		t.Fatalf("release-tag = %q", got)
	}

	if got := sink.Single("release-request"); got != "" {
		t.Fatalf("release-request = %q", got)
	}

	if !strings.Contains(out.String(), "Release tag accepted: v1.2.3") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestReleaseTagGuard_RequestTagOutputsBothValues(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appvalidate.ReleaseTagGuard(context.Background(), sink, &out, appvalidate.ReleaseTagGuardInput{Tag: "release-request/v2.3.4"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("release-tag"); got != "v2.3.4" {
		t.Fatalf("release-tag = %q", got)
	}

	if got := sink.Single("release-request"); got != "release-request/v2.3.4" {
		t.Fatalf("release-request = %q", got)
	}

	if !strings.Contains(out.String(), "Release request accepted: release-request/v2.3.4 -> v2.3.4") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestReleaseTagGuard_RejectsPrereleaseAndUnanchoredPattern(t *testing.T) {
	t.Parallel()

	err := appvalidate.ReleaseTagGuard(context.Background(), nil, &bytes.Buffer{}, appvalidate.ReleaseTagGuardInput{Tag: "v1.2.3-rc.1"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("prerelease err = %v, want ErrValidation", err)
	}

	err = appvalidate.ReleaseTagGuard(context.Background(), nil, &bytes.Buffer{}, appvalidate.ReleaseTagGuardInput{Tag: "v1.2.3", Pattern: "v[0-9]+"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("pattern err = %v, want ErrUsage", err)
	}
}

func TestChangelog_RequiredMissing(t *testing.T) {
	t.Parallel()
	path := testfs.NewReal(t).Path("CHANGELOG.md")
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, nil, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     path,
		Required: true,
	})
	// A required changelog that is absent is missing input (exit 66), which
	// tells the operator to add the file rather than to fix a flag.
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want it to say the file is not there", err)
	}

	// Nothing published: a later step must not read a changelog output that
	// the validator refused to produce.
	if got := sink.Single("content"); got != "" {
		t.Errorf("published content %q despite the refusal", got)
	}
}

func TestChangelog_RequiredPresent(t *testing.T) {
	t.Parallel()
	path := testfs.NewReal(t).WriteFile("CHANGELOG.md", []byte("# Changelog\n## v1.0.0\n- thing\n"))

	var buf bytes.Buffer

	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, nil, &buf, appvalidate.ChangelogInput{
		Path:     path,
		Required: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "Full changelog found (3 lines)") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestChangelog_MinimalAbsentEmitsSentinel(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, nil, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     testfs.NewReal(t).Path("missing.txt"),
		Required: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("content"); got != "No changes for this release" {
		t.Errorf("content = %q", got)
	}
}

func TestChangelog_MinimalPresentEmitsContent(t *testing.T) {
	t.Parallel()

	body := "Line one\nLine two\n"
	path := testfs.NewReal(t).WriteFile("minimal.txt", []byte(body))
	sink := fakeoutputsink.New(t)

	err := appvalidate.Changelog(context.Background(), sink, nil, &bytes.Buffer{}, appvalidate.ChangelogInput{
		Path:     path,
		Required: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.Multiline("content")
	if len(got) != 2 || got[0] != "Line one" || got[1] != "Line two" {
		t.Errorf("content lines = %v", got)
	}
}

// failingSink fails the nth Set call (1-based) and records every key it was
// asked to write, including the one it refused.
//
// It is local rather than an option on fakeoutputsink because only this test
// needs it: a shared fake that can fail is a fake every other test has to read
// past to be sure it isn't failing.
type failingSink struct {
	failOn int
	calls  int
	keys   []string
	err    error
}

func (s *failingSink) Set(_ context.Context, key, _ string) error {
	s.calls++
	s.keys = append(s.keys, key)

	if s.calls == s.failOn {
		return s.err
	}

	return nil
}

func (s *failingSink) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

func (s *failingSink) SetMultiline(_ context.Context, key string, _ []string) error {
	s.calls++
	s.keys = append(s.keys, key)

	if s.calls == s.failOn {
		return s.err
	}

	return nil
}

func (s *failingSink) Close(context.Context) error { return nil }

// TestReleaseTagGuard_RefusesBeforePublishingAnything proves each refusal
// happens before any output is written.
//
// The refusal tests used to pass a nil sink, and ReleaseTagGuard skips
// publication entirely when the sink is nil. So the property they looked like
// they were checking — that a rejected tag is not published — was not being
// checked at all: the code could have emitted the outputs and then refused, and
// a nil sink would have swallowed it silently. That ordering is what matters
// here, because `release-tag` is consumed by the release job that runs next. A
// tag published and then refused is a tag the next job acts on.
//
// The sink is seeded first so "nothing was written" is distinguishable from
// "the sink was never touched" — an assertion over an empty sink passes just as
// well when the guard was never called.
func TestReleaseTagGuard_RefusesBeforePublishingAnything(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   appvalidate.ReleaseTagGuardInput
		want error
		why  string
	}{
		{
			name: "a prerelease is not a stable release tag",
			in:   appvalidate.ReleaseTagGuardInput{Tag: "v1.2.3-rc.1"},
			want: errs.ErrValidation,
			why:  "rc builds must not enter the stable release path",
		},
		{
			name: "an unanchored pattern is refused as usage",
			in:   appvalidate.ReleaseTagGuardInput{Tag: "v1.2.3", Pattern: "v[0-9]+"},
			want: errs.ErrUsage,
			why:  "an unanchored pattern matches a substring, so v1.2.3-rc.1 would pass",
		},
		{
			name: "an uncompilable pattern is refused as usage",
			in:   appvalidate.ReleaseTagGuardInput{Tag: "v1.2.3", Pattern: "^v[0-9+$"},
			want: errs.ErrUsage,
			why:  "a broken pattern is operator configuration, not a bad tag",
		},
		{
			// Reaching this branch takes a degenerate pattern: it needs one
			// that rejects the raw ref but accepts the empty string left
			// after the prefix is stripped. No sane release pattern does
			// both, which is the point — the guard refuses instead of
			// publishing an empty release-tag, and that stays true whatever
			// pattern an operator supplies.
			name: "a release request with no final tag is refused",
			in:   appvalidate.ReleaseTagGuardInput{Tag: "release-request/", Pattern: "^$"},
			want: errs.ErrValidation,
			why:  "publishing an empty release-tag would hand the next job nothing to release",
		},
		{
			name: "an unrelated ref is not a release tag",
			in:   appvalidate.ReleaseTagGuardInput{Tag: "refs/heads/main"},
			want: errs.ErrValidation,
			why:  "a branch ref must not be normalised into a release",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)
			if err := sink.Set(context.Background(), "seeded", "untouched"); err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer

			err := appvalidate.ReleaseTagGuard(context.Background(), sink, &out, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v: %s", err, tc.want, tc.why)
			}

			if got := sink.Keys(); len(got) != 1 || got[0] != "seeded" {
				t.Errorf("sink keys = %v, want only the seeded key: the refusal published output", got)
			}

			if got := sink.Single("seeded"); got != "untouched" {
				t.Errorf("seeded value = %q, want %q", got, "untouched")
			}

			if strings.Contains(out.String(), "accepted") {
				t.Errorf("refusal printed success prose: %q", out.String())
			}
		})
	}
}

// errSinkFull stands in for an output sink that cannot accept a write.
var errSinkFull = errors.New("sink is full") //nolint:err113 // test fixture sentinel.

// TestReleaseTagGuard_ReportsSinkFailuresAndPublishesNoSuccess covers the two
// output writes independently, because they fail differently.
//
// There is no transaction here and this test says so rather than pretending
// otherwise: `release-tag` is written first, so a failure on `release-request`
// leaves the first output already published. Nothing can un-write it — the real
// sinks append to a runner file. What the guard owes in that case is an error
// carrying the sink's own cause and no success line, so the job fails instead
// of continuing on a half-written set of outputs.
func TestReleaseTagGuard_ReportsSinkFailuresAndPublishesNoSuccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		failOn    int
		wantKeys  []string
		published string
	}{
		{
			name:      "the first output fails",
			failOn:    1,
			wantKeys:  []string{"release-tag"},
			published: "nothing reached the sink",
		},
		{
			name:      "the second output fails after the first was written",
			failOn:    2,
			wantKeys:  []string{"release-tag", "release-request"},
			published: "release-tag is already published and cannot be withdrawn",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &failingSink{failOn: tc.failOn, err: errSinkFull}

			var out bytes.Buffer

			err := appvalidate.ReleaseTagGuard(context.Background(), sink, &out,
				appvalidate.ReleaseTagGuardInput{Tag: "refs/tags/v1.2.3"})
			if !errors.Is(err, errSinkFull) {
				t.Fatalf("err = %v, want the sink's own cause (%v) so the operator sees why publishing failed", err, errSinkFull)
			}

			if len(sink.keys) != len(tc.wantKeys) {
				t.Fatalf("sink calls = %v, want %v (%s)", sink.keys, tc.wantKeys, tc.published)
			}

			for i, want := range tc.wantKeys {
				if sink.keys[i] != want {
					t.Errorf("sink call %d = %q, want %q", i, sink.keys[i], want)
				}
			}

			if out.Len() != 0 {
				t.Errorf("a failed publish printed %q; the job must not read as succeeded", out.String())
			}
		})
	}
}
