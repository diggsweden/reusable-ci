// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appci "github.com/diggsweden/reusable-ci/internal/app/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

// errFetchMiss is the static fetch failure the fakes return (a probe miss
// or an absent ref); static so the err113 linter is satisfied.
var errFetchMiss = errors.New("fetch miss")

// fakeCheckoutGit records the calls Checkout makes and lets a test fail
// specific fetches (to drive the bare-name probe fall-through).
type fakeCheckoutGit struct {
	initFormat   string
	fetches      [][]string // refspecs of each fetch, in order
	checkouts    []string   // checkout-detach targets, in order
	failFetch    func(refspecs []string) error
	fetchedTags    bool // FetchTags was called
	failFetchTag   error
	partialClone   bool     // EnablePartialClone was called
	sparseCone     bool     // SparseInit cone arg
	sparsePatterns []string // SparseSet patterns
	headSHA        string
}

func (f *fakeCheckoutGit) InitWithObjectFormat(_ context.Context, format string) error {
	f.initFormat = format

	return nil
}

func (f *fakeCheckoutGit) RemoteAdd(_ context.Context, _, _ string) error { return nil }

func (f *fakeCheckoutGit) Fetch(_ context.Context, _ string, refspecs []string, _ string) error {
	f.fetches = append(f.fetches, refspecs)
	if f.failFetch != nil {
		return f.failFetch(refspecs)
	}

	return nil
}

func (f *fakeCheckoutGit) FetchTags(_ context.Context, _, _ string) error {
	f.fetchedTags = true

	return f.failFetchTag
}

func (f *fakeCheckoutGit) EnablePartialClone(_ context.Context) error {
	f.partialClone = true

	return nil
}

func (f *fakeCheckoutGit) SparseInit(_ context.Context, cone bool) error {
	f.sparseCone = cone

	return nil
}

func (f *fakeCheckoutGit) SparseSet(_ context.Context, patterns []string) error {
	f.sparsePatterns = patterns

	return nil
}

func (f *fakeCheckoutGit) CheckoutDetach(_ context.Context, ref string) error {
	f.checkouts = append(f.checkouts, ref)

	return nil
}

func (f *fakeCheckoutGit) RevParse(_ context.Context, _ string) (string, error) {
	if f.headSHA == "" {
		return "deadbeef", nil
	}

	return f.headSHA, nil
}

func baseInput(t *testing.T, ref string) appci.CheckoutInput {
	t.Helper()

	return appci.CheckoutInput{
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
		if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, baseInput(t, "v1.0.0")); err != nil {
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

		if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, in); err != nil {
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

		if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, in); err == nil {
			t.Error("expected error when tag fetch fails")
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

		if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, in); err != nil {
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
		if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, baseInput(t, "v1.0.0")); err != nil {
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
			git := &fakeCheckoutGit{headSHA: "resolved-sha"}
			sink := fakeoutputsink.New(t)

			got, err := appci.Checkout(context.Background(), git, sink, nil, baseInput(t, tc.ref))
			if err != nil {
				t.Fatal(err)
			}

			if len(git.fetches) != 1 || !equalStrings(git.fetches[0], tc.wantFetch) {
				t.Errorf("fetches = %v, want one fetch %v", git.fetches, tc.wantFetch)
			}

			if len(git.checkouts) != 1 || git.checkouts[0] != tc.wantCheck {
				t.Errorf("checkouts = %v, want [%s]", git.checkouts, tc.wantCheck)
			}

			if got != "resolved-sha" || sink.Single("checkout-sha") != "resolved-sha" {
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
		headSHA: "branch-sha",
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

	if _, err := appci.Checkout(context.Background(), git, sink, nil, baseInput(t, "release")); err != nil {
		t.Fatal(err)
	}

	if len(git.fetches) != 2 {
		t.Fatalf("want 2 probe fetches (tag then branch), got %v", git.fetches)
	}

	if got := git.checkouts; len(got) != 1 || got[0] != "refs/remotes/origin/release" {
		t.Errorf("checkout = %v, want branch dst", got)
	}
}

func TestCheckout_BareNameNotFound(t *testing.T) {
	git := &fakeCheckoutGit{failFetch: func([]string) error { return errFetchMiss }}
	sink := fakeoutputsink.New(t)

	_, err := appci.Checkout(context.Background(), git, sink, nil, baseInput(t, "ghost"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestCheckout_RejectsUnsafeRefs(t *testing.T) {
	for _, ref := range []string{"", "-rf", "a\nb", "a\rb"} {
		t.Run("ref="+ref, func(t *testing.T) {
			git := &fakeCheckoutGit{}
			sink := fakeoutputsink.New(t)

			_, err := appci.Checkout(context.Background(), git, sink, nil, baseInput(t, ref))
			if err == nil {
				t.Fatal("expected rejection")
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
	if _, err := appci.Checkout(ctx, &fakeCheckoutGit{}, sink, nil, noRepo); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing repository: err = %v, want ErrUsage", err)
	}

	noServer := baseInput(t, "main")

	noServer.ServerURL = ""
	if _, err := appci.Checkout(ctx, &fakeCheckoutGit{}, sink, nil, noServer); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing server-url: err = %v, want ErrUsage", err)
	}
}

func TestCheckout_RefusesExistingGitDir(t *testing.T) {
	in := baseInput(t, "main")
	if err := os.MkdirAll(filepath.Join(in.Workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := appci.Checkout(context.Background(), &fakeCheckoutGit{}, fakeoutputsink.New(t), nil, in)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (refuse checkout over existing .git)", err)
	}
}

func TestCheckout_DefaultsObjectFormatToSHA1(t *testing.T) {
	git := &fakeCheckoutGit{}
	in := baseInput(t, "refs/tags/v1") // ObjectFormat left empty

	if _, err := appci.Checkout(context.Background(), git, fakeoutputsink.New(t), nil, in); err != nil {
		t.Fatal(err)
	}

	if git.initFormat != "sha1" {
		t.Errorf("init format = %q, want sha1 default", git.initFormat)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
