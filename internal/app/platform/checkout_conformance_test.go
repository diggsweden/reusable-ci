//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// Conformance: drive the real Checkout through the real git adapter and judge
// every resolved SHA against an independent `git ls-remote` of the source,
// across the full-ref, bare-name-probe and annotated-tag resolution paths.
//
// The source is an owned bare repository. Checkout insists on an HTTP(S)
// server, so the forge URL is rewritten to that repository with a transient
// insteadOf entry, and GIT_ALLOW_PROTOCOL=file makes any unrewritten URL a
// refusal rather than a network attempt. Real-forge checkout belongs to the
// separately authorized live tier (internal/livetest/conformance).
const (
	conformanceServer  = "https://forge.invalid"
	conformanceRepo    = "owner/app"
	conformanceRepo256 = "owner/app256"

	// conformanceSecret is a synthetic credential every checkout here carries,
	// so the tests can prove no form of it is left in the workspace.
	conformanceSecret = "synthetic-checkout-secret-4b1d"
)

func TestCheckout_ConformsToAnIndependentLsRemote(t *testing.T) {
	src := isolatedgit.NewRepo(t)
	src.AddCommit("release")
	src.AddTag("v1.0.0", "release one")
	src.Git("checkout", "-q", "-b", "feature")
	src.AddCommit("feature work")
	src.Git("checkout", "-q", "main")
	src.AddCommit("after the tag")

	// A branch sharing the tag's name: a bare name must still resolve the tag.
	src.Git("branch", "v1.0.0", "feature")

	bare := src.AddBareRemote()
	src.Git("push", "-q", "origin", "feature", "refs/heads/v1.0.0", "refs/tags/v1.0.0")

	// A SHA-256 repository beside it, for the object-format path.
	bare256 := sha256BareRepository(t)

	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+bare+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", conformanceServer+"/"+conformanceRepo+".git")
	t.Setenv("GIT_CONFIG_KEY_1", "url."+bare256+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_1", conformanceServer+"/"+conformanceRepo256+".git")

	for _, tc := range []struct {
		name       string
		repository string
		source     string
		format     string
		ref        string // what we hand Checkout
		lsRef      string // the authoritative ref we ls-remote independently
	}{
		{"full branch ref", conformanceRepo, bare, "sha1", "refs/heads/main", "refs/heads/main"},
		{"bare name of a branch via the probe", conformanceRepo, bare, "sha1", "feature", "refs/heads/feature"},
		{"full annotated tag ref", conformanceRepo, bare, "sha1", "refs/tags/v1.0.0", "refs/tags/v1.0.0"},
		{"bare name of an annotated tag via the probe, over a same-named branch", conformanceRepo, bare, "sha1", "v1.0.0", "refs/tags/v1.0.0"},
		{"SHA-256 repository by branch", conformanceRepo256, bare256, "sha256", "main", "refs/heads/main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := lsRemotePeeled(t, tc.source, tc.lsRef)

			workspace := t.TempDir()
			sink := fakeoutputsink.New(t)

			var narration strings.Builder

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			got, err := appplatform.Checkout(ctx, checkoutGitFactory(&adaptergit.Repo{Dir: workspace}), sink, &narration, appplatform.CheckoutInput{
				Repository:   tc.repository,
				ServerURL:    conformanceServer,
				Ref:          tc.ref,
				Workspace:    workspace,
				ObjectFormat: tc.format,
				Token:        runcontext.OperatorCredential(conformanceSecret),
			})
			if err != nil {
				t.Fatalf("Checkout(%q): %v", tc.ref, err)
			}

			if got != want || sink.Single("checkout-sha") != want {
				t.Errorf("resolved SHA = %q, output = %q, want %q (independent ls-remote of %s)", got, sink.Single("checkout-sha"), want, tc.lsRef)
			}

			// Checkout metadata: a detached HEAD at the resolved commit, the
			// requested object format, and the credential-free forge URL as origin.
			for args, expected := range map[string]string{
				"rev-parse HEAD":                       want,
				"rev-parse --show-object-format":       tc.format,
				"config --get remote.origin.url":       conformanceServer + "/" + tc.repository + ".git",
				"rev-parse --abbrev-ref --verify HEAD": "HEAD",
			} {
				if value := gitOutput(t, workspace, strings.Fields(args)...); value != expected {
					t.Errorf("git %s = %q, want %q", args, value, expected)
				}
			}

			requireNoCredentialForm(t, workspace, narration.String(), sink.Single("checkout-sha"))
		})
	}
}

// sha256BareRepository creates an owned bare SHA-256 repository with one commit
// on main.
func sha256BareRepository(t *testing.T) string {
	t.Helper()

	work, bare := t.TempDir(), t.TempDir()
	gitOutput(t, work, "init", "-q", "-b", "main", "--object-format=sha256")
	gitOutput(t, work, "-c", "user.name=Test Bot", "-c", "user.email=bot@example.invalid", "-c", "commit.gpgsign=false",
		"commit", "-q", "--allow-empty", "-m", "sha256 fixture")
	gitOutput(t, bare, "init", "-q", "--bare", "-b", "main", "--object-format=sha256")
	gitOutput(t, work, "push", "-q", bare, "main")

	return bare
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "git", args...) //nolint:gosec // Fixed git subcommands on owned directories.
	command.Dir = dir

	out, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	return strings.TrimSpace(string(out))
}

// requireNoCredentialForm walks every file the checkout left in the workspace
// and the text it produced, and fails if the synthetic credential appears raw or
// in the Basic form git's extraheader carries.
func requireNoCredentialForm(t *testing.T, workspace string, produced ...string) {
	t.Helper()

	forms := []string{conformanceSecret, base64.StdEncoding.EncodeToString([]byte("x-access-token:" + conformanceSecret))}

	check := func(where string, content []byte) {
		for _, form := range forms {
			if bytes.Contains(content, []byte(form)) {
				t.Errorf("%s holds a form of the checkout credential", where)
			}
		}
	}

	for _, text := range produced {
		check("checkout output", []byte(text))
	}

	if err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // Files under the test's own workspace.
		if readErr != nil {
			return readErr
		}

		check(path, content)

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// lsRemotePeeled returns the commit the ref resolves to in the source,
// preferring the peeled ^{} line of an annotated tag. An absent ref or a
// failing ls-remote fails the test: the fixture is owned, so there is nothing
// to skip for.
func lsRemotePeeled(t *testing.T, repository, ref string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "ls-remote", repository, ref+"^{}", ref).Output() //nolint:gosec // owned repository and fixed refs.
	if err != nil {
		t.Fatalf("git ls-remote %s %s: %v", repository, ref, err)
	}

	var fallback string

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if strings.HasSuffix(fields[1], "^{}") {
			return fields[0]
		}

		if fallback == "" {
			fallback = fields[0]
		}
	}

	if fallback == "" {
		t.Fatalf("git ls-remote %s found no %s", repository, ref)
	}

	return fallback
}
