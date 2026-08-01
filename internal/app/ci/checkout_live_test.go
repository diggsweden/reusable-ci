//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appci "github.com/diggsweden/reusable-ci/v3/internal/app/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// Live conformance: drive the real Checkout (real git adapter) against a
// public repo over HTTPS and assert the resolved SHA matches an
// independent `git ls-remote` — the authoritative source of truth — across
// the full-ref, bare-name-probe, and tag resolution paths. This is the
// reusable-ci-side correctness guarantee; the shell-vs-CLI byte-equality
// check lives forgejo-ci-side (its checkout-consumer.sh is not in this
// repo). Network-gated: skips cleanly when the repo is unreachable.
const (
	liveServer  = "https://github.com"
	liveRepo    = "diggsweden/reusable-ci"
	liveRepoURL = liveServer + "/" + liveRepo
)

func TestCheckout_LiveConformance(t *testing.T) {
	cases := []struct {
		name      string
		ref       string // what we hand Checkout
		lsRef     string // the authoritative ref we ls-remote independently
		probePath bool   // bare-name → exercises the tag-then-branch probe
	}{
		{"full branch ref", "refs/heads/main", "refs/heads/main", false},
		{"bare name (branch via probe)", "main", "refs/heads/main", true},
		{"full tag ref", "refs/tags/v3.0.0-pre", "refs/tags/v3.0.0-pre", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := lsRemotePeeled(t, liveRepoURL, tc.lsRef)
			if want == "" {
				t.Skipf("offline or %s unreachable; skipping live conformance", liveRepoURL)
			}

			workspace := t.TempDir()
			repo := &adaptergit.Repo{Dir: workspace}
			sink := fakeoutputsink.New(t)

			got, err := appci.Checkout(context.Background(), repo, sink, io.Discard, appci.CheckoutInput{
				Repository:   liveRepo,
				ServerURL:    liveServer,
				Ref:          tc.ref,
				Workspace:    workspace,
				ObjectFormat: "sha1", // GitHub repos are sha1
			})
			if err != nil {
				t.Fatalf("Checkout(%q): %v", tc.ref, err)
			}

			if got != want {
				t.Errorf("resolved SHA = %q, want %q (independent ls-remote of %s)", got, want, tc.lsRef)
			}

			if sink.Single("checkout-sha") != want {
				t.Errorf("output sink = %q, want %q", sink.Single("checkout-sha"), want)
			}

			// Credential-free guarantee on the live path too.
			cfg, readErr := os.ReadFile(filepath.Join(workspace, ".git", "config"))
			if readErr != nil {
				t.Fatal(readErr)
			}

			if strings.Contains(string(cfg), "extraheader") {
				t.Error(".git/config retained an extraheader — checkout must stay credential-free")
			}
		})
	}
}

// lsRemotePeeled returns the commit SHA the ref resolves to on the remote,
// peeling annotated tags (preferring the ^{} line). Returns "" when the
// remote is unreachable or the ref is absent, so callers can skip offline.
func lsRemotePeeled(t *testing.T, url, ref string) string {
	t.Helper()

	out, err := exec.Command("git", "ls-remote", url, ref+"^{}", ref).CombinedOutput() //nolint:gosec,noctx // test infra; url/ref are constants.
	if err != nil {
		return ""
	}

	var fallback string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if strings.HasSuffix(fields[1], "^{}") {
			return fields[0] // peeled commit of an annotated tag
		}

		if fallback == "" {
			fallback = fields[0]
		}
	}

	return fallback
}
