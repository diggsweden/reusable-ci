// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeGit struct {
	runErr      error
	tags        []string
	tagsErr     error
	shortSHAOut string
	shortSHAErr error

	// captures
	runArgs    [][]string
	listTagsAt []string
	shortRefs  []string
}

func (f *fakeGit) Run(_ context.Context, args ...string) (string, error) {
	f.runArgs = append(f.runArgs, args)

	return "", f.runErr
}

func (f *fakeGit) ListTags(_ context.Context, pattern string) ([]string, error) {
	f.listTagsAt = append(f.listTagsAt, pattern)

	return f.tags, f.tagsErr
}

func (f *fakeGit) ShortSHA(_ context.Context, ref string, _ int) (string, error) {
	f.shortRefs = append(f.shortRefs, ref)

	return f.shortSHAOut, f.shortSHAErr
}

func TestGenerateSnapshotVersion_HappyPath(t *testing.T) {
	ops := &fakeGit{
		tags:        []string{"v0.4.0", "v0.5.9", "v0.5.0"},
		shortSHAOut: "abc1234", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "feat/awesome",
	}); err != nil {
		t.Fatalf("GenerateSnapshotVersion: %v", err)
	}

	// v0.5.9 is the highest of the three, not the first or the last: the
	// fixture is deliberately unsorted so a version picked by position fails.
	got := strings.TrimSpace(out.String())
	if got != "0.5.9-snapshot-feat-awesome-abc1234" {
		t.Errorf("got %q", got)
	}

	// These recordings existed but were never asserted. The tag pattern is
	// what keeps a stray "v1.0.0-rc1" or a "snapshot/..." tag out of the
	// baseline, and the SHA is taken from HEAD rather than from the tag.
	if !slices.Equal(ops.listTagsAt, []string{"v[0-9]*.[0-9]*.[0-9]*"}) {
		t.Errorf("ListTags patterns = %v", ops.listTagsAt)
	}

	if !slices.Equal(ops.shortRefs, []string{"HEAD"}) {
		t.Errorf("ShortSHA refs = %v, want HEAD", ops.shortRefs)
	}
}

func TestGenerateSnapshotVersion_NoTagsFallsBackToZero(t *testing.T) {
	ops := &fakeGit{
		shortSHAOut: "deadbee",
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatalf("GenerateSnapshotVersion: %v", err)
	}

	got := strings.TrimSpace(out.String())
	if got != "0.0.0-snapshot-main-deadbee" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSnapshotVersion_FetchFailureIgnored(t *testing.T) {
	ops := &fakeGit{
		runErr:      errors.New("offline"), //nolint:err113 // test mock error
		tags:        []string{"v1.0.0"},
		shortSHAOut: "1234567",
	}

	var out bytes.Buffer
	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
	}); err != nil {
		t.Fatalf("expected fetch failure to be ignored: %v", err)
	}

	if !strings.Contains(out.String(), "1.0.0-snapshot-main-1234567") {
		t.Errorf("output = %q", out.String())
	}

	// The fetch was attempted and its failure swallowed -- not skipped. A
	// version generated without refreshing tags can name a stale baseline.
	if !slices.EqualFunc(ops.runArgs, [][]string{{"fetch", "--tags"}}, slices.Equal) {
		t.Errorf("git calls = %v, want one `fetch --tags`", ops.runArgs)
	}
}

func TestGenerateSnapshotVersion_ShortSHAErrorBubbles(t *testing.T) {
	notARepo := errors.New("not a git repo") //nolint:err113 // test mock error
	ops := &fakeGit{
		tags:        []string{"v1.0.0"},
		shortSHAErr: notARepo,
	}

	var out bytes.Buffer

	err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
	})
	// Unlike the fetch above, this one is fatal: a version without the commit
	// it was built from is not a snapshot version. The cause has to survive.
	if !errors.Is(err, notARepo) {
		t.Fatalf("err = %v, want it to wrap %v", err, notARepo)
	}

	// And nothing half-formed was printed for a caller to consume.
	if out.Len() != 0 {
		t.Errorf("emitted %q despite the failure", out.String())
	}
}

func TestGenerateSnapshotVersion_RequiresRefName(t *testing.T) {
	ops := &fakeGit{}

	err := appversion.GenerateSnapshotVersion(context.Background(), ops, &bytes.Buffer{}, appversion.GenerateSnapshotVersionInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Refused before touching git.
	if len(ops.runArgs) != 0 {
		t.Errorf("git ran without a ref name: %v", ops.runArgs)
	}
}

func TestGenerateSnapshotVersion_JSONFormat_EmitsObject(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{
		tags:        []string{"v1.2.3"},
		shortSHAOut: "abc1234",
	}

	var out bytes.Buffer

	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
		Format:  output.FormatJSON,
	}); err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}

	if want := "1.2.3-snapshot-main-abc1234"; decoded.Version != want {
		t.Errorf("version = %q, want %q", decoded.Version, want)
	}
}

func TestGenerateSnapshotVersion_TextFormat_PreservesBareLine(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{tags: []string{"v0.1.0"}, shortSHAOut: "deadbee"}

	var out bytes.Buffer

	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "main",
		Format:  output.FormatText,
	}); err != nil {
		t.Fatal(err)
	}

	if want := "0.1.0-snapshot-main-deadbee\n"; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}
}

func TestGenerateSnapshotVersion_GitHubFormatEmitsOutput(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{tags: []string{"v0.2.0"}, shortSHAOut: "abc1234"}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "feature/demo",
		Format:  output.FormatGitHub,
		Sink:    sink,
	}); err != nil {
		t.Fatal(err)
	}

	const want = "0.2.0-snapshot-feature-demo-abc1234"
	if got := sink.Single("snapshot-version"); got != want {
		t.Errorf("snapshot-version = %q, want %q", got, want)
	}

	if out.String() != want+"\n" {
		t.Errorf("out = %q, want %q", out.String(), want+"\n")
	}
}

// TestGenerateSnapshotVersion_EveryFailureWritesNothing covers each way the
// version cannot be produced or delivered. A ref name the sanitizer empties
// and a short SHA that is not an abbreviated commit are refused -- both used
// to compose a version with an empty or garbage segment -- the tag listing
// failure is returned, and a sink that refuses the output stops the version
// line from being printed as if it had been delivered.
func TestGenerateSnapshotVersion_EveryFailureWritesNothing(t *testing.T) {
	t.Parallel()

	errListFailed := errors.New("list tags failed")   //nolint:err113 // a unique value to find in the chain.
	errSinkClosed := errors.New("output file closed") //nolint:err113 // a unique value to find in the chain.

	for name, tc := range map[string]struct {
		ops      *fakeGit
		refName  string
		sinkErr  error
		wantErr  error
		wantRuns int
	}{
		"ref name the sanitizer empties": {ops: &fakeGit{shortSHAOut: "abc1234"}, refName: "///", wantErr: errs.ErrUsage},
		"empty short sha":                {ops: &fakeGit{}, refName: "main", wantErr: errs.ErrMalformedInput, wantRuns: 1},
		"short sha too short":            {ops: &fakeGit{shortSHAOut: "abc12"}, refName: "main", wantErr: errs.ErrMalformedInput, wantRuns: 1},
		"short sha not hex":              {ops: &fakeGit{shortSHAOut: "fatal: x"}, refName: "main", wantErr: errs.ErrMalformedInput, wantRuns: 1},
		"tag listing fails":              {ops: &fakeGit{tagsErr: errListFailed, shortSHAOut: "abc1234"}, refName: "main", wantErr: errListFailed, wantRuns: 1},
		"sink refuses the output":        {ops: &fakeGit{shortSHAOut: "abc1234"}, refName: "main", sinkErr: errSinkClosed, wantErr: errSinkClosed, wantRuns: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &refusingSink{Sink: fakeoutputsink.New(t), err: tc.sinkErr}

			var out bytes.Buffer

			err := appversion.GenerateSnapshotVersion(context.Background(), tc.ops, &out, appversion.GenerateSnapshotVersionInput{
				RefName: tc.refName, Format: output.FormatGitLab, Sink: sink,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}

			if out.Len() != 0 || len(sink.Keys()) != 0 || len(tc.ops.runArgs) != tc.wantRuns {
				t.Errorf("out = %q, outputs = %v, git runs = %v; want nothing written and %d runs", out.String(), sink.Keys(), tc.ops.runArgs, tc.wantRuns)
			}
		})
	}
}

// TestGenerateSnapshotVersion_ExactGitCallsAndGitLabDelivery pins what is
// asked of git and what a GitLab run receives. The tag listing uses the
// strict semver glob and the abbreviation is taken from HEAD; an eight-digit
// abbreviation (git lengthens one to keep it unique) is used as given.
func TestGenerateSnapshotVersion_ExactGitCallsAndGitLabDelivery(t *testing.T) {
	t.Parallel()

	ops := &fakeGit{tags: []string{"v1.2.3", "v1.10.0", "v1.9.9-rc.1"}, shortSHAOut: "abc12345"}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appversion.GenerateSnapshotVersion(context.Background(), ops, &out, appversion.GenerateSnapshotVersionInput{
		RefName: "feature/Mixed Case#1", Format: output.FormatGitLab, Sink: sink,
	}); err != nil {
		t.Fatal(err)
	}

	const want = "1.10.0-snapshot-feature-Mixed-Case-1-abc12345"

	if !slices.EqualFunc(ops.runArgs, [][]string{{"fetch", "--tags"}}, slices.Equal) ||
		!slices.Equal(ops.listTagsAt, []string{"v[0-9]*.[0-9]*.[0-9]*"}) || !slices.Equal(ops.shortRefs, []string{"HEAD"}) {
		t.Errorf("git calls: run %q, list %q, short %q", ops.runArgs, ops.listTagsAt, ops.shortRefs)
	}

	if got := sink.AllScalar(); len(got) != 1 || got["snapshot-version"] != want || out.String() != want+"\n" {
		t.Errorf("outputs = %v, out = %q; want only snapshot-version=%s", got, out.String(), want)
	}
}

type refusingSink struct {
	*fakeoutputsink.Sink

	err error
}

func (r *refusingSink) Set(ctx context.Context, key, value string) error {
	if r.err != nil {
		return r.err
	}

	return r.Sink.Set(ctx, key, value)
}
