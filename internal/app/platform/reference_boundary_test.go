// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type refRecorder struct {
	fakeRefGit
	calls [][]string
}

func (f *refRecorder) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, slices.Clone(args))

	return f.out, f.err
}

func TestResolveBoundary_BoundRowsAndExactCall(t *testing.T) {
	t.Parallel()

	one, two, long := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64)
	for _, tc := range []struct{ name, ref, rows, want string }{
		{"branch", "main", one + "\trefs/heads/main\n", one},
		{"tag", "v1", one + "\trefs/tags/v1\n", one},
		{"peel", "v1", one + "\trefs/tags/v1\n" + two + "\trefs/tags/v1^{}\n", two},
		{"reverse peel", "v1", two + "\trefs/tags/v1^{}\n" + one + "\trefs/tags/v1\n", two},
		{"explicit tag", "refs/tags/v1", one + "\trefs/tags/v1", one},
		{"explicit branch", "refs/heads/main", one + "\trefs/heads/main", one},
		{"head", "HEAD", one + "\tHEAD", one},
		{"sha1", one, "", one}, {"sha256", long, "", long},
		{"sha256 row", "main", long + "\trefs/heads/main", long},
		{"identical duplicate", "main", one + "\trefs/heads/main\n" + one + "\trefs/heads/main", one},
		{"short object", "main", "abc123\trefs/heads/main", ""},
		{"invalid object", "main", strings.Repeat("z", 40) + "\trefs/heads/main", ""},
		{"conflicting duplicate", "main", one + "\trefs/heads/main\n" + two + "\trefs/heads/main", ""},
		{"ambiguous", "main", one + "\trefs/heads/main\n" + two + "\trefs/tags/main", ""},
		{"ambiguous reversed", "main", two + "\trefs/tags/main\n" + one + "\trefs/heads/main", ""},
		{"unbound peel", "v1", two + "\trefs/tags/v1^{}", ""},
		{"unrelated peel", "main", two + "\trefs/tags/other^{}", ""},
		{"unrelated", "main", one + "\trefs/heads/other", ""},
		{"mixed unrelated", "main", one + "\trefs/heads/main\n" + two + "\trefs/heads/other", ""},
		{"extra column", "main", one + "\trefs/heads/main\textra", ""},
		{"mixed format", "v1", one + "\trefs/tags/v1\n" + long + "\trefs/tags/v1^{}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &refRecorder{fakeRefGit: fakeRefGit{out: tc.rows}}
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			got, err := appplatform.ResolveRef(t.Context(), recorder, sink, &out, appplatform.ResolveRefInput{RemoteURL: "https://forge.example/owner/repo.git", Ref: tc.ref, OutputKey: "selected-sha"})

			patterns := []string{tc.ref}
			if tc.ref == "v1" || tc.ref == "main" {
				patterns = []string{"refs/heads/" + tc.ref, "refs/tags/" + tc.ref, "refs/tags/" + tc.ref + "^{}"}
			}

			if tc.ref == "refs/tags/v1" {
				patterns = append(patterns, "refs/tags/v1^{}")
			}

			wantCall := append([]string{"ls-remote", "--", "https://forge.example/owner/repo.git"}, patterns...)
			if !reflect.DeepEqual(recorder.calls, [][]string{wantCall}) {
				t.Fatalf("calls=%v", recorder.calls)
			}

			if tc.want != "" {
				if err != nil || got != tc.want || sink.Single("selected-sha") != tc.want || out.String() != tc.want+"\n" {
					t.Fatalf("got=%s err=%v out=%s", got, err, &out)
				}
			} else if err == nil || got != "" || len(sink.Keys()) != 0 || out.Len() != 0 {
				t.Fatalf("bad rows published: got=%s err=%v keys=%v out=%s", got, err, sink.Keys(), &out)
			}
		})
	}
}

func TestResolveBoundary_RefusalAndDependencyIdentity(t *testing.T) {
	t.Parallel()

	for _, in := range []appplatform.ResolveRefInput{
		{RemoteURL: "--upload-pack=unexpected", Ref: "main"}, {RemoteURL: "https://forge.example/o/r\nextra", Ref: "main"},
		{RemoteURL: "https://forge.example/o/r", Ref: "--heads"}, {RemoteURL: "https://forge.example/o/r", Ref: "main*"},
		{RemoteURL: "https://forge.example/o/r", Ref: "HEAD~1"}, {RemoteURL: "https://forge.example/o/r", Ref: "a\xffb"},
		{RemoteURL: "https://forge.example/o/r", Ref: "main", OutputKey: "bad\nkey"},
	} {
		recorder := &refRecorder{}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appplatform.ResolveRef(t.Context(), recorder, sink, &out, in)
		if err == nil || len(recorder.calls) != 0 || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("err=%v calls=%v keys=%v out=%s", err, recorder.calls, sink.Keys(), &out)
		}
	}

	recorder := &refRecorder{fakeRefGit: fakeRefGit{err: errRefLookupOffline}}

	_, err := appplatform.ResolveRef(t.Context(), recorder, fakeoutputsink.New(t), &bytes.Buffer{}, appplatform.ResolveRefInput{RemoteURL: "https://forge.example/o/r", Ref: "main"})
	if !errors.Is(err, errRefLookupOffline) || len(recorder.calls) != 1 {
		t.Fatalf("err=%v calls=%v", err, recorder.calls)
	}
}

func TestCheckoutBoundary_CompleteOrderCredentialsAndFailures(t *testing.T) {
	t.Parallel()

	const remote = "https://codeberg.org/owner/repo.git"

	cred := runcontext.OperatorCredential("owned-checkout-marker")

	want := []checkoutEvent{
		{method: "init", args: []string{"sha1"}}, {method: "remote", args: []string{"origin", remote}},
		{method: "partial"}, {method: "sparse-init", args: []string{"true"}}, {method: "sparse-set", args: []string{"scripts/bootstrap", "src"}},
		{method: "all", args: []string{remote}, credential: cred}, {method: "probe-tag", args: []string{"origin", "release"}, credential: cred},
		{method: "probe-branch", args: []string{"origin", "release"}, credential: cred},
		{method: "fetch", args: []string{remote, "7", "+refs/heads/release:refs/remotes/origin/release"}, credential: cred},
		{method: "detach", args: []string{"refs/remotes/origin/release"}}, {method: "tags", args: []string{remote}, credential: cred},
		{method: "rev-parse", args: []string{"HEAD"}},
	}
	for fail := 0; fail <= len(want); fail++ {
		t.Run(strconv.Itoa(fail), func(t *testing.T) {
			cause := fmt.Errorf("stage %d: %w", fail, errFetchMiss)
			git := &fakeCheckoutGit{tagMissing: true, failAt: fail, failure: cause}
			in := baseInput(t, "release")
			in.Depth = 7
			in.Sparse = []string{"scripts/bootstrap", "src"}
			in.FetchAllRefs = true
			in.FetchTags = true
			in.Token = cred
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			got, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, in)
			in.Sparse[0] = "changed-after-call"

			expected := want
			if fail > 0 {
				expected = want[:fail]
			}

			if !reflect.DeepEqual(git.events, expected) {
				t.Fatalf("event methods/arguments/credentials differed at stage %d: got=%d want=%d", fail, len(git.events), len(expected))
			}

			if fail == 0 {
				if err != nil || got != strings.Repeat("d", 40) || sink.Single("checkout-sha") != got || out.String() != "Checked out owner/repo at "+got+" (sha1)\n" {
					t.Fatalf("err=%v got=%s out=%s", err, got, &out)
				}
			} else if !errors.Is(err, cause) || got != "" || len(sink.Keys()) != 0 || out.Len() != 0 {
				t.Fatalf("err=%v got=%s keys=%v out=%s", err, got, sink.Keys(), &out)
			}

			if strings.Contains(out.String(), "owned-checkout-marker") {
				t.Fatal("credential in output")
			}
		})
	}
}

func TestCheckoutBoundary_InputsAndObjectFormats(t *testing.T) {
	t.Parallel()

	for _, change := range []func(*appplatform.CheckoutInput){
		func(in *appplatform.CheckoutInput) { in.OutputKey = "bad\nkey" }, func(in *appplatform.CheckoutInput) { in.Depth = -1 },
		func(in *appplatform.CheckoutInput) { in.ObjectFormat = "sha512" }, func(in *appplatform.CheckoutInput) { in.Ref = strings.Repeat("a", 64) },
		func(in *appplatform.CheckoutInput) { in.ObjectFormat = "sha256"; in.Ref = strings.Repeat("a", 40) },
		func(in *appplatform.CheckoutInput) { in.FetchBase = "x:y" }, func(in *appplatform.CheckoutInput) { in.FetchBase = "bad*" },
		func(in *appplatform.CheckoutInput) { in.Sparse = []string{"../other"} }, func(in *appplatform.CheckoutInput) { in.Sparse = []string{"--no-cone"} },
		func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src", "src"} }, func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src/../other"} },
		func(in *appplatform.CheckoutInput) { in.Sparse = []string{""} }, func(in *appplatform.CheckoutInput) { in.Sparse = []string{"/absolute"} },
		func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src*"} }, func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src\tbad"} },
		func(in *appplatform.CheckoutInput) { in.Repository = "owner/../other" }, func(in *appplatform.CheckoutInput) { in.Repository = "owner/repo.git" },
		func(in *appplatform.CheckoutInput) { in.ServerURL = "https://user:password@forge.example" }, func(in *appplatform.CheckoutInput) { in.ServerURL = "https://forge.example?other" },
	} {
		in := baseInput(t, "main")
		in.Workspace = filepath.Join(in.Workspace, "new-checkout")
		change(&in)

		git := &fakeCheckoutGit{}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, in)
		if err == nil || len(git.events) != 0 || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("err=%v events=%v out=%s", err, git.events, &out)
		}

		if _, err := os.Stat(in.Workspace); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refusal created checkout directory")
		}
	}

	for _, head := range []string{"deadbeef", strings.Repeat("a", 41), strings.Repeat("b", 64), strings.Repeat("G", 40)} {
		git := &fakeCheckoutGit{headSHA: head}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, baseInput(t, "main"))
		if !errors.Is(err, errs.ErrValidation) || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("head=%q err=%v out=%s", head, err, &out)
		}
	}
}

func TestCheckoutBoundary_ProbeFailuresNeverBecomeMisses(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{errs.ErrPermissionDenied, errs.ErrDependencyUnavailable, context.Canceled, context.DeadlineExceeded, os.ErrPermission, errRefLookupOffline, errs.ErrMissingInput} {
		git := &fakeCheckoutGit{failAt: 3, failure: cause}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, baseInput(t, "main"))
		if !errors.Is(err, cause) || len(git.events) != 3 || len(git.fetches) != 0 || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("cause=%v err=%v events=%d", cause, err, len(git.events))
		}
	}
}

func TestWorkspaceBoundary_LinksAndLogSafety(t *testing.T) {
	fsys := testfs.NewReal(t)
	outside := testfs.NewReal(t)
	outside.WriteFile("private-canary", []byte("unrelated"))

	if err := os.Symlink(outside.Root, fsys.Path(".github-shared")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: fsys.Root}); err == nil || out.Len() != 0 {
		t.Fatalf("err=%v output=%s", err, &out)
	}

	if err := os.Remove(fsys.Path(".github-shared")); err != nil {
		t.Fatal(err)
	}

	fsys.WriteFile("bad\nname", []byte("owned"))

	if err := appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: fsys.Root, ActionRef: "main\nforged-line", ActionRepository: "name\x1b[2J"}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "bad\nname") || strings.Contains(out.String(), "main\nforged-line") || strings.ContainsRune(out.String(), '\x1b') || !strings.Contains(out.String(), `bad\nname`) {
		t.Fatalf("unsafe/missing diagnostic=%q", out.String())
	}
}

func TestCheckoutBoundary_BaseFetchAndWorkspaceLinks(t *testing.T) {
	t.Parallel()
	in := baseInput(t, "refs/tags/v1.0.0")
	in.FetchBase = "main"
	in.Token = runcontext.OperatorCredential("base-fetch-marker")

	git := &fakeCheckoutGit{}
	if _, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
		t.Fatal(err)
	}

	want := checkoutEvent{method: "fetch", args: []string{"https://codeberg.org/owner/repo.git", "0", "+refs/heads/main:refs/heads/main"}, credential: in.Token}
	if len(git.events) != 7 || !reflect.DeepEqual(git.events[5], want) {
		t.Fatal("base branch fetch lost source, credential, depth or refspec")
	}

	for _, name := range []string{"workspace", ".git"} {
		root := t.TempDir()
		outside := t.TempDir()

		target := filepath.Join(root, name)
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}

		in.Workspace = root
		if name == "workspace" {
			in.Workspace = target
		}

		git = &fakeCheckoutGit{}

		sink := fakeoutputsink.New(t)
		if _, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, nil, in); !errors.Is(err, errs.ErrValidation) || len(git.events) != 0 || len(sink.Keys()) != 0 {
			t.Fatalf("linked checkout err=%v events=%d", err, len(git.events))
		}
	}
}
