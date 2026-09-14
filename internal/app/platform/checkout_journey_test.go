// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type checkoutJournal struct {
	*fakeoutputsink.Sink
	bytes.Buffer
	events []checkoutEvent
	record func(context.Context, checkoutEvent) error
}

func (j *checkoutJournal) Set(ctx context.Context, key, value string) error {
	if err := j.record(ctx, checkoutEvent{method: "sink", args: []string{key, value}}); err != nil {
		return err
	}

	return j.Sink.Set(ctx, key, value)
}

func (j *checkoutJournal) Write(body []byte) (int, error) {
	// The diagnostic writer can accept a prefix before failing, independently of
	// the already accepted machine value. No imaginary rollback is modeled.
	if err := j.record(nil, checkoutEvent{method: "writer", args: []string{string(body)}}); err != nil { //nolint:staticcheck // io.Writer has no context; the callback handles writer events separately.
		count, _ := j.Buffer.Write(body[:5])

		return count, err
	}

	return j.Buffer.Write(body)
}

func TestCheckoutJourney_OrderedFailuresAndRetainedDestination(t *testing.T) { //nolint:gocognit,gocyclo,maintidx // one oracle pins all branch policies, failure positions and publication states.
	t.Parallel()

	const remote = "https://forge.example/team/sub/repo.git"

	cred := runcontext.OperatorCredential("owned-journey-credential")

	for _, policy := range []struct {
		name, ref, format, refspec, detach string
		tagMissing, branchMissing          bool
		probes                             []string
	}{
		{"explicit-tag", "refs/tags/release", "sha1", "+refs/tags/release:refs/tags/release", "refs/tags/release", false, false, nil},
		{"explicit-branch", "refs/heads/release", "sha1", "+refs/heads/release:refs/remotes/origin/release", "refs/remotes/origin/release", false, false, nil},
		{"sha1", strings.Repeat("a", 40), "sha1", strings.Repeat("a", 40), "FETCH_HEAD", false, false, nil},
		{"sha256", strings.Repeat("b", 64), "sha256", strings.Repeat("b", 64), "FETCH_HEAD", false, false, nil},
		{"bare-tag-precedence", "release", "sha1", "+refs/tags/release:refs/tags/release", "refs/tags/release", false, false, []string{"probe-tag"}},
		{"bare-branch", "release", "sha1", "+refs/heads/release:refs/remotes/origin/release", "refs/remotes/origin/release", true, false, []string{"probe-tag", "probe-branch"}},
		{"neither", "release", "sha1", "", "", true, true, []string{"probe-tag", "probe-branch"}},
	} {
		for _, extras := range []string{"minimal", "base-present", "base-absent"} {
			sha := strings.Repeat("d", 40)
			if policy.format == "sha256" {
				sha = strings.Repeat("e", 64)
			}

			key := "checkout-sha"
			want := []checkoutEvent{{method: "init", args: []string{policy.format}}, {method: "remote", args: []string{"origin", remote}}}

			if extras != "minimal" {
				key = "selected-commit"

				want = append(want, checkoutEvent{method: "partial"}, checkoutEvent{method: "sparse-init", args: []string{"true"}},
					checkoutEvent{method: "sparse-set", args: []string{"src", "scripts/bootstrap"}}, checkoutEvent{method: "all", args: []string{remote}, credential: cred})
			}

			for _, probe := range policy.probes {
				want = append(want, checkoutEvent{method: probe, args: []string{"origin", "release"}, credential: cred})
			}

			if policy.name != "neither" {
				want = append(want, checkoutEvent{method: "fetch", args: []string{remote, "7", policy.refspec}, credential: cred}, checkoutEvent{method: "detach", args: []string{policy.detach}})
				if extras != "minimal" {
					want = append(want, checkoutEvent{method: "probe-branch", args: []string{"origin", "base"}, credential: cred})
					if extras == "base-present" {
						want = append(want, checkoutEvent{method: "fetch", args: []string{remote, "0", "+refs/heads/base:refs/heads/base"}, credential: cred})
					}

					want = append(want, checkoutEvent{method: "tags", args: []string{remote}, credential: cred})
				}

				want = append(want, checkoutEvent{method: "rev-parse", args: []string{"HEAD"}}, checkoutEvent{method: "sink", args: []string{key, sha}},
					checkoutEvent{method: "writer", args: []string{"Checked out team/sub/repo at " + sha + " (" + policy.format + ")\n"}})
			}

			causes := make([]error, len(want))
			for index, event := range want {
				causes[index] = fmt.Errorf("%s/%s/%d/%s", policy.name, extras, index, event.method) //nolint:err113 // every reachable position has an independent identity, including repeated Fetch/probe calls.
			}

			for _, existing := range []bool{false, true} {
				for fail := 0; fail <= len(want); fail++ {
					t.Run(policy.name+"/"+extras+"/existing="+strconv.FormatBool(existing)+"/fail="+strconv.Itoa(fail), func(t *testing.T) {
						ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(time.Minute))
						defer cancel()

						root := t.TempDir()
						workspace := filepath.Join(root, "checkout")
						canary := filepath.Join(root, "unrelated")

						if existing {
							require.NoError(t, os.Mkdir(workspace, 0o755))
							require.NoError(t, os.WriteFile(filepath.Join(workspace, "state"), []byte("before checkout"), 0o600))
							canary = filepath.Join(workspace, "unrelated")
						}

						require.NoError(t, os.WriteFile(canary, []byte("caller data"), 0o600))
						before, err := os.Stat(canary)
						require.NoError(t, err)

						in := appplatform.CheckoutInput{Repository: "team/sub/repo", ServerURL: "https://forge.example", Ref: policy.ref, Workspace: workspace, ObjectFormat: policy.format, Token: cred, Depth: 7}
						if extras != "minimal" {
							in.FetchBase, in.FetchTags, in.FetchAllRefs = "base", true, true
							in.Sparse, in.OutputKey = []string{"src", "scripts/bootstrap"}, key
						}

						journal := &checkoutJournal{Sink: fakeoutputsink.New(t)}
						git := &fakeCheckoutGit{headSHA: sha, tagMissing: policy.tagMissing, branchMissing: policy.branchMissing}
						workDir := workspace
						gitFactory := func(dir string) appplatform.CheckoutGit {
							workDir = dir

							return git
						}
						journal.record = func(callCtx context.Context, event checkoutEvent) error {
							if event.method != "writer" {
								require.Same(t, ctx, callCtx, "context identity, cancellation and deadline must survive")
							}

							journal.events = append(journal.events, event)
							if event.method != "sink" && event.method != "writer" {
								// A failing Git operation may already have changed the directory it
								// is working in. Which directory that is, is the point of staging.
								require.NoError(t, os.WriteFile(filepath.Join(workDir, ".git"), []byte("inert owned Git marker"), 0o600))
								require.NoError(t, os.WriteFile(filepath.Join(workDir, "state"), []byte(strconv.Itoa(len(journal.events))+":"+event.method), 0o600))
							}

							if event.method == "probe-branch" && event.args[1] == "base" {
								git.branchMissing = extras == "base-absent"
							}

							if fail > 0 && len(journal.events) == fail {
								return causes[fail-1]
							}

							return nil
						}
						git.onCall = journal.record

						got, err := appplatform.Checkout(ctx, gitFactory, journal, journal, in)
						if len(in.Sparse) != 0 {
							in.Sparse[0] = "changed-after-call"
						}

						expected := want

						switch {
						case fail > 0:
							expected = want[:fail]
							require.ErrorIs(t, err, causes[fail-1])
							require.Empty(t, got)
						case policy.name == "neither":
							require.ErrorIs(t, err, errs.ErrValidation)
							require.Empty(t, got)
						default:
							require.NoError(t, err)
							require.Equal(t, sha, got)
						}

						for index, cause := range causes {
							if index != fail-1 {
								require.NotErrorIs(t, err, cause)
							}
						}

						require.Equal(t, expected, journal.events, "single Git/sink/writer stream")

						lastGit := len(expected)
						for lastGit > 0 && (expected[lastGit-1].method == "sink" || expected[lastGit-1].method == "writer") {
							lastGit--
						}

						// Where the partial state ends up is the whole difference staging
						// makes. A destination this run created is never published unless
						// the checkout finished, so a failure leaves neither it nor the
						// staging sibling behind and a retry starts from nothing. A
						// destination the caller already had is worked in place, and
						// keeps whatever Git managed to write, as it always did.
						published := err == nil
						if !existing && !published {
							_, statErr := os.Stat(workspace)
							require.ErrorIs(t, statErr, os.ErrNotExist, "a failed checkout must not publish a destination")

							siblings, readErr := os.ReadDir(root)
							require.NoError(t, readErr)
							require.Len(t, siblings, 1, "the staging directory must be removed: %v", siblings)
							require.Equal(t, "unrelated", siblings[0].Name())
						} else {
							state, readErr := os.ReadFile(filepath.Join(workspace, "state"))
							require.NoError(t, readErr)
							require.Equal(t, strconv.Itoa(lastGit)+":"+expected[lastGit-1].method, string(state), "retain actual partial state, not rollback")

							marker, readErr := os.ReadFile(filepath.Join(workspace, ".git"))
							require.NoError(t, readErr)
							require.Equal(t, "inert owned Git marker", string(marker))
						}

						body, readErr := os.ReadFile(canary)
						require.NoError(t, readErr)
						require.Equal(t, "caller data", string(body))

						after, statErr := os.Stat(canary)
						require.NoError(t, statErr)
						require.True(t, os.SameFile(before, after))
						require.Equal(t, before.Mode(), after.Mode())

						if slices.ContainsFunc(expected, func(event checkoutEvent) bool { return event.method == "writer" }) {
							require.Equal(t, []string{key}, journal.Keys())
							require.Equal(t, sha, journal.Single(key))

							text := want[len(want)-1].args[0]
							if fail > 0 {
								text = text[:5]
							}

							require.Equal(t, text, journal.String())
						} else {
							require.Empty(t, journal.Keys())
							require.Empty(t, journal.String())
						}

						retry := &fakeCheckoutGit{}
						_, retryErr := appplatform.Checkout(ctx, checkoutGitFactory(retry), fakeoutputsink.New(t), nil, in)

						if !existing && !published {
							// Nothing was published, so the retry is not a retry into a
							// half-finished tree: it starts over and reaches Git.
							require.NotEmpty(t, retry.events, "a failed staged checkout must permit a clean retry")

							return
						}
						// A .git that survived, whether from the caller or from a run that
						// worked in place, deliberately prevents an unqualified retry.
						require.ErrorIs(t, retryErr, errs.ErrValidation)
						require.Empty(t, retry.events)
					})
				}
			}
		}
	}
}
