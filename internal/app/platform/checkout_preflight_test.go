// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestCheckoutPreflight_AllLocalFieldsBeforeCreation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*appplatform.CheckoutInput)
		cause  error
	}{
		{"repository-required", func(in *appplatform.CheckoutInput) { in.Repository = "" }, errs.ErrUsage},
		{"repository-path", func(in *appplatform.CheckoutInput) { in.Repository = "team/../repo" }, errs.ErrUsage},
		{"repository-escape", func(in *appplatform.CheckoutInput) { in.Repository = "team/%2e/repo" }, errs.ErrUsage},
		{"server-required", func(in *appplatform.CheckoutInput) { in.ServerURL = "" }, errs.ErrUsage},
		{"server-authority", func(in *appplatform.CheckoutInput) { in.ServerURL = "https://user:password@forge.example" }, errs.ErrUsage},
		{"server-fragment", func(in *appplatform.CheckoutInput) { in.ServerURL = "https://forge.example#fragment" }, errs.ErrUsage},
		{"server-port", func(in *appplatform.CheckoutInput) { in.ServerURL = "https://forge.example:0" }, errs.ErrUsage},
		{"server-control", func(in *appplatform.CheckoutInput) { in.ServerURL = "https://forge.example\n" }, errs.ErrUsage},
		{"ref-required", func(in *appplatform.CheckoutInput) { in.Ref = "" }, errs.ErrUsage},
		{"ref-expression", func(in *appplatform.CheckoutInput) { in.Ref = "main~1" }, errs.ErrValidation},
		{"ref-option", func(in *appplatform.CheckoutInput) { in.Ref = "--all" }, errs.ErrValidation},
		{"ref-control", func(in *appplatform.CheckoutInput) { in.Ref = "main\x1b" }, errs.ErrValidation},
		{"ref-encoding", func(in *appplatform.CheckoutInput) { in.Ref = "main\xff" }, errs.ErrValidation},
		{"format", func(in *appplatform.CheckoutInput) { in.ObjectFormat = "sha512" }, errs.ErrUsage},
		{"sha1-mismatch", func(in *appplatform.CheckoutInput) { in.ObjectFormat = "sha1"; in.Ref = strings.Repeat("b", 64) }, errs.ErrValidation},
		{"sha256-mismatch", func(in *appplatform.CheckoutInput) { in.ObjectFormat = "sha256"; in.Ref = strings.Repeat("a", 40) }, errs.ErrValidation},
		{"depth", func(in *appplatform.CheckoutInput) { in.Depth = -1 }, errs.ErrUsage},
		{"base-option", func(in *appplatform.CheckoutInput) { in.FetchBase = "--base" }, errs.ErrValidation},
		{"base-expression", func(in *appplatform.CheckoutInput) { in.FetchBase = "base~1" }, errs.ErrValidation},
		{"sparse-escape", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"../src"} }, errs.ErrValidation},
		{"sparse-glob", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src*"} }, errs.ErrValidation},
		{"sparse-duplicate", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src", "src"} }, errs.ErrValidation},
		{"sparse-noncanonical", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src/../other"} }, errs.ErrValidation},
		{"sparse-encoding", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src\xff"} }, errs.ErrValidation},
		{"sparse-option", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"--stdin"} }, errs.ErrValidation},
		{"sparse-absolute", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"/src"} }, errs.ErrValidation},
		{"sparse-empty", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src", ""} }, errs.ErrValidation},
		{"sparse-root", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"."} }, errs.ErrValidation},
		{"sparse-trailing-slash", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src/"} }, errs.ErrValidation},
		{"sparse-question", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"sr?"} }, errs.ErrValidation},
		{"sparse-class", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"[s]rc"} }, errs.ErrValidation},
		{"sparse-backslash", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src\\x"} }, errs.ErrValidation},
		{"sparse-control", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src\ttab"} }, errs.ErrValidation},
		{"sparse-later-duplicate", func(in *appplatform.CheckoutInput) { in.Sparse = []string{"src", "docs", "src"} }, errs.ErrValidation},
		{"ref-glob", func(in *appplatform.CheckoutInput) { in.Ref = "release/*" }, errs.ErrValidation},
		{"ref-double-dot", func(in *appplatform.CheckoutInput) { in.Ref = "refs/heads/a..b" }, errs.ErrValidation},
		{"ref-empty-component", func(in *appplatform.CheckoutInput) { in.Ref = "release//x" }, errs.ErrValidation},
		{"ref-lock-suffix", func(in *appplatform.CheckoutInput) { in.Ref = "release.lock" }, errs.ErrValidation},
		{"ref-at-brace", func(in *appplatform.CheckoutInput) { in.Ref = "main@{1}" }, errs.ErrValidation},
		{"ref-tab", func(in *appplatform.CheckoutInput) { in.Ref = "main\tx" }, errs.ErrValidation},
		{"base-control", func(in *appplatform.CheckoutInput) { in.FetchBase = "base\x7f" }, errs.ErrValidation},
		{"base-absolute-ref", func(in *appplatform.CheckoutInput) { in.FetchBase = "refs/heads/../x" }, errs.ErrValidation},
		{"output", func(in *appplatform.CheckoutInput) { in.OutputKey = "output\nkey" }, errs.ErrUsage},
		{"workspace-required", func(in *appplatform.CheckoutInput) { in.Workspace = "" }, errs.ErrUsage},
		{"workspace-whitespace", func(in *appplatform.CheckoutInput) { in.Workspace = "   " }, errs.ErrUsage},
		{"workspace-parent", func(in *appplatform.CheckoutInput) { in.Workspace += "/../other" }, errs.ErrValidation},
		{"workspace-nul-after-missing", func(in *appplatform.CheckoutInput) { in.Workspace += "/bad\x00path" }, errs.ErrUsage},
		{"workspace-encoding", func(in *appplatform.CheckoutInput) { in.Workspace += "/bad\xffpath" }, errs.ErrUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			in := appplatform.CheckoutInput{Repository: "team/sub/repo", ServerURL: "https://forge.example/base", Ref: "release", Workspace: filepath.Join(root, "missing", "checkout"), Depth: 7, FetchBase: "base", FetchTags: true, FetchAllRefs: true, Sparse: []string{"src", "scripts/bootstrap"}, OutputKey: "selected-sha"}
			tc.change(&in)
			require.ErrorIs(t, appplatform.PreflightCheckout(in), tc.cause)
			git, sink := &fakeCheckoutGit{}, fakeoutputsink.New(t)

			var out bytes.Buffer

			sha, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, &out, in)
			require.ErrorIs(t, err, tc.cause)
			require.Empty(t, sha)
			require.Empty(t, git.events)
			require.Empty(t, sink.Keys())
			require.Empty(t, out.String())

			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestCheckoutPreflight_DefersOnlyUnknownSHALength(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"", "sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			in := appplatform.CheckoutInput{Repository: "team/sub/repo", ServerURL: "https://forge.example/base", Ref: strings.Repeat("b", 64), Workspace: filepath.Join(root, "missing", "checkout"), ObjectFormat: format}

			err := appplatform.PreflightCheckout(in)
			if format == "sha1" {
				require.ErrorIs(t, err, errs.ErrValidation)
			} else {
				require.NoError(t, err)
			}

			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Empty(t, entries, "even successful preflight must not create directories")
			git, sink := &fakeCheckoutGit{}, fakeoutputsink.New(t)

			_, err = appplatform.Checkout(t.Context(), checkoutGitFactory(git), sink, nil, in)
			if format != "sha256" {
				require.ErrorIs(t, err, errs.ErrValidation)
				require.Empty(t, git.events)
				require.Empty(t, sink.Keys())

				entries, err = os.ReadDir(root)
				require.NoError(t, err)
				require.Empty(t, entries)
			} else {
				require.NoError(t, err)
				require.Equal(t, "sha256", git.initFormat)
				require.Equal(t, strings.Repeat("d", 64), sink.Single("checkout-sha"))
			}
		})
	}
}

// TestCheckoutDestination_UnusableIsRefusedNotStaged separates the two reasons
// a destination cannot be opened. Absent means this run creates it, and creating
// it is what earns the staged, atomic path. Anything else means something is
// already there that this run does not understand, and staging beside it would
// publish a rename over whatever that was.
func TestCheckoutDestination_UnusableIsRefusedNotStaged(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		arrange func(t *testing.T, root, workspace string)
	}{
		{
			name: "the destination is a file",
			arrange: func(t *testing.T, _, workspace string) {
				t.Helper()
				require.NoError(t, os.WriteFile(workspace, []byte("caller data"), 0o600))
			},
		},
		{
			name: "the destination is reached through a link",
			arrange: func(t *testing.T, root, workspace string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(root, "elsewhere"), 0o755))
				require.NoError(t, os.Symlink(filepath.Join(root, "elsewhere"), workspace))
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			workspace := filepath.Join(root, "checkout")
			testCase.arrange(t, root, workspace)
			before := ownedCheckoutTree(t, root)

			git := &fakeCheckoutGit{}
			_, err := appplatform.Checkout(t.Context(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, appplatform.CheckoutInput{
				Repository: "team/repo", ServerURL: "https://forge.example", Ref: "main", Workspace: workspace,
			})

			require.Error(t, err)
			require.Empty(t, git.events, "an unusable destination is refused before any Git call")
			require.Equal(t, before, ownedCheckoutTree(t, root), "a refusal must not stage, publish or remove anything")
		})
	}
}

// ownedCheckoutTree records every entry under root without following links, so
// a refusal that quietly created or removed something is visible.
func ownedCheckoutTree(t *testing.T, root string) map[string]string {
	t.Helper()

	state := map[string]string{}

	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}

		state[path] = info.Mode().String()

		return nil
	}))

	return state
}
