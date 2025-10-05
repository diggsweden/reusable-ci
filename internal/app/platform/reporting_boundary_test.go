// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var errReportingBoundary = errors.New("owned reporting failure")

type reportingSink struct {
	*fakeoutputsink.Sink
	events *[]string
	fail   bool
}

func (s *reportingSink) Set(ctx context.Context, key, value string) error {
	*s.events = append(*s.events, "sink")
	if s.fail {
		return errReportingBoundary
	}

	return s.Sink.Set(ctx, key, value)
}

type reportingWriter struct {
	bytes.Buffer
	events *[]string
	fail   bool
}

func (w *reportingWriter) Write(body []byte) (int, error) {
	if w.events != nil {
		*w.events = append(*w.events, "writer")
	}

	if w.fail {
		return 0, errReportingBoundary
	}

	return w.Buffer.Write(body)
}

func TestPlatformReportingBoundary_ExactOutputAndCommitOrder(t *testing.T) { //nolint:gocognit // both public commands share the sink-first contract, with independent writer and sink failures.
	t.Parallel()

	for _, checkout := range []bool{false, true} {
		for _, failure := range []string{"none", "writer", "sink"} {
			var events []string

			sink := &reportingSink{Sink: fakeoutputsink.New(t), events: &events, fail: failure == "sink"}
			writer := &reportingWriter{events: &events, fail: failure == "writer"}
			sha := strings.Repeat("d", 40)
			want := sha + "\n"
			key := "selected-sha"

			var (
				got string
				err error
			)

			if checkout {
				git := &fakeCheckoutGit{}
				in := baseInput(t, "refs/heads/main")
				in.OutputKey = key
				got, err = appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, writer, in)
				want = "Checked out owner/repo at " + sha + " (sha1)\n"

				last := git.events[len(git.events)-1]
				if last.method != "rev-parse" || !reflect.DeepEqual(last.args, []string{"HEAD"}) {
					t.Fatalf("HEAD lookup=%+v", last)
				}
			} else {
				got, err = appplatform.ResolveRef(t.Context(), fakeRefGit{out: sha + "\trefs/heads/main"}, sink, writer, appplatform.ResolveRefInput{RemoteURL: "https://forge.example/o/r", Ref: "main", OutputKey: key})
			}

			wantEvents := []string{"sink", "writer"}
			if failure == "sink" {
				wantEvents = []string{"sink"}
			}

			if !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("publication order=%v", events)
			}

			if failure == "none" {
				if err != nil || got != sha || writer.String() != want {
					t.Fatalf("got=%s err=%v text=%q", got, err, writer.String())
				}
			} else if !errors.Is(err, errReportingBoundary) || got != "" {
				t.Fatalf("failure=%s err=%v got=%s", failure, err, got)
			}

			if failure == "sink" {
				if len(sink.Keys()) != 0 || writer.Len() != 0 {
					t.Fatal("sink refusal wrote output")
				}
			} else if sink.Single(key) != sha {
				t.Fatal("committed machine value lost")
			}
		}
	}
}

func TestWorkspaceReportingBoundary_CompleteListingsAndErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	first := fsys.WriteFile("root-only.txt", []byte("root"))
	second := fsys.WriteFile(".github-shared/shared-only.txt", []byte("shared"))

	if err := os.Chmod(first, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(second, 0o600); err != nil {
		t.Fatal(err)
	}

	in := appplatform.DebugWorkspaceInput{Root: fsys.Root, ActionRepository: "org/engine", ActionRef: "ref"}

	var out bytes.Buffer
	if err := appplatform.DebugWorkspace(&out, in); err != nil {
		t.Fatal(err)
	}

	rootLine := "-rw-------        4 root-only.txt\n"

	sharedLine := "-rw-------        6 shared-only.txt\n"
	if !strings.Contains(out.String(), rootLine) || !strings.Contains(out.String(), sharedLine) || strings.Index(out.String(), rootLine) > strings.Index(out.String(), sharedLine) {
		t.Fatalf("listing=%s", &out)
	}

	var again bytes.Buffer
	if err := appplatform.DebugWorkspace(&again, in); err != nil || out.String() != again.String() {
		t.Fatal("listing not deterministic")
	}

	if err := appplatform.DebugWorkspace(&reportingWriter{fail: true}, in); !errors.Is(err, errReportingBoundary) {
		t.Fatalf("writer error=%v", err)
	}

	var missing bytes.Buffer
	if err := appplatform.DebugWorkspace(&missing, appplatform.DebugWorkspaceInput{Root: fsys.Path("missing")}); err == nil || missing.Len() != 0 {
		t.Fatalf("missing-root err=%v text=%s", err, &missing)
	}
}

func TestFetchBaseBoundary_AbsenceErrorsAndForwarding(t *testing.T) {
	t.Parallel()

	cred := runcontext.OperatorCredential("owned-base-credential")

	for _, state := range []string{"present", "absent", "lookup failure", "fetch failure", "cancelled"} {
		git := &fakeCheckoutGit{}
		in := baseInput(t, "refs/heads/release")
		in.FetchBase = "main"
		in.FetchTags = true
		in.FetchAllRefs = true
		in.Depth = 3
		in.Token = cred

		switch state {
		case "absent":
			git.branchMissing = true
		case "lookup failure":
			git.failAt = 6
			git.failure = errReportingBoundary
		case "fetch failure":
			git.failAt = 7
			git.failure = errReportingBoundary
		case "cancelled":
			git.failAt = 6
			git.failure = context.Canceled
		}

		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, in)
		if state == "present" || state == "absent" { //nolint:nestif // positive existence and typed absence intentionally take different fetch paths.
			if err != nil || !git.fetchedTags {
				t.Fatalf("state=%s err=%v", state, err)
			}

			if state == "present" {
				want := checkoutEvent{method: "fetch", args: []string{"https://codeberg.org/owner/repo.git", "0", "+refs/heads/main:refs/heads/main"}, credential: cred}
				if !reflect.DeepEqual(git.events[6], want) {
					t.Fatalf("base fetch=%+v", git.events[6])
				}
			} else if len(git.fetches) != 1 {
				t.Fatal("absent base was fetched")
			}
		} else if !errors.Is(err, git.failure) || len(git.events) != git.failAt || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("state=%s err=%v events=%v", state, err, git.events)
		}

		probe := git.events[5]
		if probe.method != "probe-branch" || !reflect.DeepEqual(probe.args, []string{"origin", "main"}) || probe.credential.For("https://codeberg.org") != "owned-base-credential" {
			t.Fatalf("probe=%+v", probe)
		}
	}
}

func TestCheckoutGrammarBoundary_PseudoSHAsUseNamedRefDispatch(t *testing.T) {
	t.Parallel()

	for _, length := range []int{41, 48, 63} {
		ref := strings.Repeat("a", length)

		git := &fakeCheckoutGit{}
		if _, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, baseInput(t, ref)); err != nil {
			t.Fatal(err)
		}

		if git.events[2].method != "probe-tag" || len(git.checkouts) != 1 || git.checkouts[0] != "refs/tags/"+ref {
			t.Fatalf("pseudo-SHA used immutable dispatch: %v", git.events)
		}
	}

	for _, ref := range []string{"a..b", "a@{1}", "refs/heads/a.lock", "a?b", "a\\b", "refs/heads/.hidden"} {
		git := &fakeCheckoutGit{}

		sink := fakeoutputsink.New(t)
		if _, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, nil, baseInput(t, ref)); !errors.Is(err, errs.ErrValidation) || len(git.events) != 0 || len(sink.Keys()) != 0 {
			t.Fatalf("ref=%s err=%v events=%v", ref, err, git.events)
		}
	}
}
