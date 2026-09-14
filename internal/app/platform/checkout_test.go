// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// errFetchMiss is the static fetch failure the fakes return (a probe miss
// or an absent ref); static so the err113 linter is satisfied.
var errFetchMiss = errors.New("fetch miss")

// fakeCheckoutGit records the calls Checkout makes and lets a test fail
// specific fetches (to drive the bare-name probe fall-through).
type fakeCheckoutGit struct {
	onCall         func(context.Context, checkoutEvent) error
	events         []checkoutEvent
	failAt         int
	failure        error
	tagMissing     bool
	branchMissing  bool
	initFormat     string
	fetches        [][]string // refspecs of each fetch, in order
	fetchDepths    []int      // depth arg of each fetch, in order (parallel to fetches)
	checkouts      []string   // checkout-detach targets, in order
	failFetch      func(refspecs []string) error
	fetchedTags    bool // FetchTags was called
	fetchedAllRefs bool // FetchAllRefs was called
	failFetchTag   error
	partialClone   bool     // EnablePartialClone was called
	sparseCone     bool     // SparseInit cone arg
	sparsePatterns []string // SparseSet patterns
	headSHA        string
}

type checkoutEvent struct {
	method     string
	args       []string
	credential runcontext.Credential
}

func (f *fakeCheckoutGit) record(ctx context.Context, method string, cred runcontext.Credential, args ...string) error { //nolint:funcorder // the event recording primitive is documented before the fake port methods using it.
	f.events = append(f.events, checkoutEvent{method: method, args: slices.Clone(args), credential: cred})
	if f.onCall != nil {
		if err := f.onCall(ctx, f.events[len(f.events)-1]); err != nil {
			return err
		}
	}

	if len(f.events) == f.failAt {
		return f.failure
	}

	return nil
}

func (f *fakeCheckoutGit) InitWithObjectFormat(ctx context.Context, format string) error {
	f.initFormat = format

	return f.record(ctx, "init", runcontext.Credential{}, format)
}

func (f *fakeCheckoutGit) RemoteAdd(ctx context.Context, name, url string) error {
	return f.record(ctx, "remote", runcontext.Credential{}, name, url)
}

func (f *fakeCheckoutGit) Fetch(ctx context.Context, url string, refspecs []string, cred runcontext.Credential, depth int) error {
	if err := f.record(ctx, "fetch", cred, append([]string{url, strconv.Itoa(depth)}, refspecs...)...); err != nil {
		return err
	}

	f.fetches = append(f.fetches, slices.Clone(refspecs))
	f.fetchDepths = append(f.fetchDepths, depth)

	if f.failFetch != nil {
		return f.failFetch(refspecs)
	}

	return nil
}

func (f *fakeCheckoutGit) FetchTags(ctx context.Context, url string, cred runcontext.Credential) error {
	if err := f.record(ctx, "tags", cred, url); err != nil {
		return err
	}

	f.fetchedTags = true

	return f.failFetchTag
}

func (f *fakeCheckoutGit) FetchAllRefs(ctx context.Context, url string, cred runcontext.Credential) error {
	f.fetchedAllRefs = true

	return f.record(ctx, "all", cred, url)
}

func (f *fakeCheckoutGit) EnablePartialClone(ctx context.Context) error {
	f.partialClone = true

	return f.record(ctx, "partial", runcontext.Credential{})
}

func (f *fakeCheckoutGit) SparseInit(ctx context.Context, cone bool) error {
	f.sparseCone = cone

	return f.record(ctx, "sparse-init", runcontext.Credential{}, strconv.FormatBool(cone))
}

func (f *fakeCheckoutGit) SparseSet(ctx context.Context, patterns []string) error {
	f.sparsePatterns = slices.Clone(patterns)

	return f.record(ctx, "sparse-set", runcontext.Credential{}, patterns...)
}

func (f *fakeCheckoutGit) CheckoutDetach(ctx context.Context, ref string) error {
	f.checkouts = append(f.checkouts, ref)

	return f.record(ctx, "detach", runcontext.Credential{}, ref)
}

func (f *fakeCheckoutGit) RevParse(ctx context.Context, ref string) (string, error) {
	if err := f.record(ctx, "rev-parse", runcontext.Credential{}, ref); err != nil {
		return "", err
	}

	if f.headSHA == "" {
		if f.initFormat == "sha256" {
			return strings.Repeat("d", 64), nil
		}

		return strings.Repeat("d", 40), nil
	}

	return f.headSHA, nil
}

func (f *fakeCheckoutGit) RemoteTagCommitIfExists(ctx context.Context, remote, ref string, cred runcontext.Credential) (string, bool, error) {
	err := f.record(ctx, "probe-tag", cred, remote, ref)

	return strings.Repeat("d", 40), !f.tagMissing, err
}
func (f *fakeCheckoutGit) RemoteBranchCommit(ctx context.Context, remote, ref string, cred runcontext.Credential) (string, bool, error) {
	err := f.record(ctx, "probe-branch", cred, remote, ref)

	return strings.Repeat("d", 40), !f.branchMissing, err
}

func baseInput(t *testing.T, ref string) appplatform.CheckoutInput {
	t.Helper()

	return appplatform.CheckoutInput{
		Repository: "owner/repo",
		ServerURL:  "https://codeberg.org",
		Ref:        ref,
		Workspace:  t.TempDir(),
	}
}

func TestCheckout_FetchTags(t *testing.T) {
	t.Parallel()

	t.Run("off by default", func(t *testing.T) {
		t.Parallel()

		git := &fakeCheckoutGit{}
		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, baseInput(t, "v1.0.0")); err != nil {
			t.Fatal(err)
		}

		if git.fetchedTags {
			t.Error("FetchTags called without opt-in")
		}
	})

	t.Run("opt-in fetches tags", func(t *testing.T) {
		t.Parallel()

		in := baseInput(t, "v1.0.0")
		in.FetchTags = true
		git := &fakeCheckoutGit{}

		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
			t.Fatal(err)
		}

		if !git.fetchedTags {
			t.Error("FetchTags not called despite opt-in")
		}
	})

	t.Run("tag fetch failure is fatal", func(t *testing.T) {
		t.Parallel()

		in := baseInput(t, "v1.0.0")
		in.FetchTags = true
		git := &fakeCheckoutGit{failFetchTag: errFetchMiss}

		// The tag fetch is an explicit opt-in, so its failure is the run's
		// failure -- unlike the bare-name probe, where a miss is expected.
		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); !errors.Is(err, errFetchMiss) {
			t.Errorf("err = %v, want the fetch failure surfaced", err)
		}
	})
}

func TestCheckout_FetchAllRefs(t *testing.T) {
	t.Parallel()

	t.Run("off by default", func(t *testing.T) {
		t.Parallel()

		git := &fakeCheckoutGit{}
		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, baseInput(t, "main")); err != nil {
			t.Fatal(err)
		}

		if git.fetchedAllRefs {
			t.Error("FetchAllRefs called without opt-in")
		}
	})

	t.Run("opt-in fetches all refs before the targeted checkout", func(t *testing.T) {
		t.Parallel()

		in := baseInput(t, "main")
		in.FetchAllRefs = true
		git := &fakeCheckoutGit{}

		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
			t.Fatal(err)
		}

		if !git.fetchedAllRefs {
			t.Error("FetchAllRefs not called despite opt-in")
		}

		// The targeted fetch+checkout still runs afterwards so the exact ref is pinned.
		if len(git.checkouts) == 0 {
			t.Error("targeted checkout must still run after the all-refs fetch")
		}
	})
}

func TestCheckout_Depth(t *testing.T) {
	t.Parallel()

	t.Run("default is full history (depth 0)", func(t *testing.T) {
		t.Parallel()

		git := &fakeCheckoutGit{}
		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, baseInput(t, "v1.0.0")); err != nil {
			t.Fatal(err)
		}

		for i, d := range git.fetchDepths {
			if d != 0 {
				t.Errorf("fetch %d depth = %d, want 0 (full history) by default", i, d)
			}
		}
	})

	t.Run("threads the requested depth to the primary fetch", func(t *testing.T) {
		t.Parallel()

		in := baseInput(t, "v1.0.0")
		in.Depth = 1
		git := &fakeCheckoutGit{}

		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
			t.Fatal(err)
		}

		if len(git.fetchDepths) == 0 || git.fetchDepths[0] != 1 {
			t.Errorf("primary fetch depths = %v, want first = 1", git.fetchDepths)
		}
	})
}

func TestCheckout_Sparse(t *testing.T) {
	t.Parallel()

	t.Run("configures cone before checkout", func(t *testing.T) {
		t.Parallel()

		in := baseInput(t, "v1.0.0")
		in.Sparse = []string{"scripts/bootstrap"}
		git := &fakeCheckoutGit{}

		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
			t.Fatal(err)
		}

		if !git.partialClone {
			t.Error("partial clone not enabled alongside sparse")
		}

		if !git.sparseCone {
			t.Error("sparse-checkout not initialized in cone mode")
		}

		if len(git.sparsePatterns) != 1 || git.sparsePatterns[0] != "scripts/bootstrap" {
			t.Errorf("sparse patterns = %v", git.sparsePatterns)
		}

		// And the fetch/checkout still happened (the cone restricts the tree,
		// it does not skip materialization).
		if len(git.checkouts) != 1 {
			t.Errorf("expected one checkout, got %v", git.checkouts)
		}
	})

	t.Run("absent leaves sparse off", func(t *testing.T) {
		t.Parallel()

		git := &fakeCheckoutGit{}
		if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, baseInput(t, "v1.0.0")); err != nil {
			t.Fatal(err)
		}

		if git.sparseCone || git.sparsePatterns != nil {
			t.Error("sparse configured without patterns")
		}
	})
}

func TestCheckout_ResolutionOrder(t *testing.T) {
	cases := []struct {
		name      string
		ref       string
		wantFetch []string // refspecs of the single expected fetch
		wantCheck string   // checkout-detach target
	}{
		{"full tag ref", "refs/tags/v1.0.0", []string{"+refs/tags/v1.0.0:refs/tags/v1.0.0"}, "refs/tags/v1.0.0"},
		{"full branch ref", "refs/heads/main", []string{"+refs/heads/main:refs/remotes/origin/main"}, "refs/remotes/origin/main"},
		{"sha1", strings.Repeat("a", 40), []string{strings.Repeat("a", 40)}, "FETCH_HEAD"},
		{"sha256", strings.Repeat("b", 64), []string{strings.Repeat("b", 64)}, "FETCH_HEAD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantSHA := strings.Repeat("d", 40)

			in := baseInput(t, tc.ref)
			if len(tc.ref) == 64 {
				in.ObjectFormat = "sha256"
				wantSHA = strings.Repeat("d", 64)
			}

			git := &fakeCheckoutGit{headSHA: wantSHA}
			sink := fakeoutputsink.New(t)

			got, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), sink, nil, in)
			if err != nil {
				t.Fatal(err)
			}

			if len(git.fetches) != 1 || !slices.Equal(git.fetches[0], tc.wantFetch) {
				t.Errorf("fetches = %v, want one fetch %v", git.fetches, tc.wantFetch)
			}

			if len(git.checkouts) != 1 || git.checkouts[0] != tc.wantCheck {
				t.Errorf("checkouts = %v, want [%s]", git.checkouts, tc.wantCheck)
			}

			if got != wantSHA || sink.Single("checkout-sha") != wantSHA || git.initFormat != map[int]string{40: "sha1", 64: "sha256"}[len(wantSHA)] {
				t.Errorf("sha = %q, sink = %q", got, sink.Single("checkout-sha"))
			}
		})
	}
}

// TestCheckout_BareNameProbesTagThenBranch drives the fall-through: the
// tag probe fails (no such tag), the branch probe succeeds. A probe miss
// must not surface as an error from Checkout.
func TestCheckout_BareNameProbesTagThenBranch(t *testing.T) {
	git := &fakeCheckoutGit{
		headSHA:    strings.Repeat("d", 40),
		tagMissing: true,
		failFetch: func(refspecs []string) error {
			for _, rs := range refspecs {
				if strings.Contains(rs, "refs/tags/") {
					return errFetchMiss
				}
			}

			return nil
		},
	}
	sink := fakeoutputsink.New(t)

	if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), sink, nil, baseInput(t, "release")); err != nil {
		t.Fatal(err)
	}

	if len(git.fetches) != 1 {
		t.Fatalf("only the existing branch should be fetched, got %v", git.fetches)
	}

	if got := git.checkouts; len(got) != 1 || got[0] != "refs/remotes/origin/release" {
		t.Errorf("checkout = %v, want branch dst", got)
	}
}

func TestCheckout_BareNameNotFound(t *testing.T) {
	git := &fakeCheckoutGit{tagMissing: true, branchMissing: true}
	sink := fakeoutputsink.New(t)

	_, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), sink, nil, baseInput(t, "ghost"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestCheckout_RejectsUnsafeRefs pins the two classes apart. An absent ref
// is the caller forgetting a flag (ErrUsage, exit 2); a ref that is
// present but would be read by git as an option, or would split a command
// line, is the value being unusable (ErrValidation, exit 65). Every ref
// here reaches a git argv, so none may get as far as a fetch.
func TestCheckout_RejectsUnsafeRefs(t *testing.T) {
	for name, testCase := range map[string]struct {
		ref     string
		wantErr error
	}{
		"no ref at all":     {ref: "", wantErr: errs.ErrUsage},
		"leading dash":      {ref: "-rf", wantErr: errs.ErrValidation},
		"embedded newline":  {ref: "a\nb", wantErr: errs.ErrValidation},
		"embedded carriage": {ref: "a\rb", wantErr: errs.ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			git := &fakeCheckoutGit{}
			sink := fakeoutputsink.New(t)

			_, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), sink, nil, baseInput(t, testCase.ref))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ref %q: err = %v, want %v", testCase.ref, err, testCase.wantErr)
			}

			if len(git.fetches) != 0 {
				t.Error("must reject before any fetch")
			}
		})
	}
}

func TestCheckout_RequiresRepositoryAndServer(t *testing.T) {
	sink := fakeoutputsink.New(t)
	ctx := context.Background()

	noRepo := baseInput(t, "main")

	noRepo.Repository = ""
	if _, err := appplatform.Checkout(ctx, checkoutGitFactory(&fakeCheckoutGit{}), sink, nil, noRepo); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing repository: err = %v, want ErrUsage", err)
	}

	noServer := baseInput(t, "main")

	noServer.ServerURL = ""
	if _, err := appplatform.Checkout(ctx, checkoutGitFactory(&fakeCheckoutGit{}), sink, nil, noServer); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing server-url: err = %v, want ErrUsage", err)
	}
}

func TestCheckout_RefusesExistingGitDir(t *testing.T) {
	in := baseInput(t, "main")
	if err := os.MkdirAll(filepath.Join(in.Workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := appplatform.Checkout(context.Background(), checkoutGitFactory(&fakeCheckoutGit{}), fakeoutputsink.New(t), nil, in)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (refuse checkout over existing .git)", err)
	}
}

func TestCheckout_DefaultsObjectFormatToSHA1(t *testing.T) {
	git := &fakeCheckoutGit{}
	in := baseInput(t, "refs/tags/v1") // ObjectFormat left empty

	if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
		t.Fatal(err)
	}

	if git.initFormat != "sha1" {
		t.Errorf("init format = %q, want sha1 default", git.initFormat)
	}
}

// checkoutGitFactory adapts one fake to the factory the app now asks for. The
// directory it is handed is the staging sibling on a fresh destination and the
// destination itself otherwise; a recording fake answers the same either way.
func checkoutGitFactory(git appplatform.CheckoutGit) func(string) appplatform.CheckoutGit {
	return func(string) appplatform.CheckoutGit { return git }
}
