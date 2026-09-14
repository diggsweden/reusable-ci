// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

var errCheckoutCommandStop = errors.New("owned checkout init stop")

// requireCheckoutWorkDir accepts the two places Git may legitimately run: the
// destination itself when it already existed, or the staging sibling this run
// created beside it and renames into place only on success.
func requireCheckoutWorkDir(t *testing.T, workspace, dir string) {
	t.Helper()

	if dir == workspace {
		return
	}

	require.Equal(t, filepath.Dir(workspace), filepath.Dir(dir), "staging must be a sibling of the destination")
	require.True(t, strings.HasPrefix(filepath.Base(dir), "."+filepath.Base(workspace)+"-staging-"),
		"unexpected checkout work directory %q for destination %q", dir, workspace)
}

type checkoutMetadataRecorder struct {
	provider.Provider
	fetch func(context.Context, string) (*provider.RepoMetadata, error)
}

func (p checkoutMetadataRecorder) FetchRepoMetadata(ctx context.Context, repo string) (*provider.RepoMetadata, error) {
	return p.fetch(ctx, repo)
}

type checkoutInitRecorder struct {
	appplatform.CheckoutGit
	init func(context.Context, string) error
}

func (g checkoutInitRecorder) InitWithObjectFormat(ctx context.Context, format string) error {
	return g.init(ctx, format)
}

type checkoutCommandProbe struct {
	events []string
	fetch  func(context.Context, string) (*provider.RepoMetadata, error)
	sink   *fakeoutputsink.Sink
}

func (p *checkoutCommandProbe) command(t *testing.T, ctx context.Context, workspace string) *cli.Command {
	t.Helper()
	p.sink = fakeoutputsink.New(t)

	return checkoutCommand(func(callCtx context.Context, _ *cli.Command, action func(*deps.Deps) error) error {
		require.Equal(t, ctx.Done(), callCtx.Done())

		p.events = append(p.events, "deps")

		return action(&deps.Deps{OutputSink: p.sink, Provider: checkoutMetadataRecorder{fetch: func(fetchCtx context.Context, repo string) (*provider.RepoMetadata, error) {
			require.Equal(t, ctx.Done(), fetchCtx.Done())

			p.events = append(p.events, "metadata:"+repo)

			return p.fetch(fetchCtx, repo)
		}}})
	}, func(dir string) appplatform.CheckoutGit {
		requireCheckoutWorkDir(t, workspace, dir)

		return checkoutInitRecorder{init: func(initCtx context.Context, format string) error {
			require.Equal(t, ctx.Done(), initCtx.Done())

			p.events = append(p.events, "init:"+format)

			return errCheckoutCommandStop
		}}
	})
}

func checkoutCommandEnv(t *testing.T) {
	t.Helper()

	keys := []string{"CHECKOUT_OBJECT_FORMAT", "CHECKOUT_FETCH_BASE", "CHECKOUT_FETCH_TAGS", "CHECKOUT_FETCH_ALL_REFS", "CHECKOUT_FETCH_DEPTH", "CHECKOUT_SPARSE", "CHECKOUT_PATH", "OUTPUT_KEY"}
	for _, variable := range []runcontext.Var{runcontext.Repository(), runcontext.ServerURL(), runcontext.CheckoutRef(), runcontext.Workspace()} {
		keys = append(keys, variable.Keys()...)
	}

	keys = append(keys, runcontext.Token().Keys()...)
	for _, key := range keys {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
	// Even a wiring regression cannot discover a host Git executable.
	t.Setenv("PATH", t.TempDir())
}

func TestCheckoutCommand_LocalPreflightBeforeMetadata(t *testing.T) { //nolint:paralleltest // tests intentionally control CLI environment sources.
	checkoutCommandEnv(t)

	for _, tc := range []struct{ flag, value string }{
		{flagRepository, ""}, {flagRepository, "repo"}, {flagRepository, "owner/../repo"}, {flagRepository, "owner/repo.git"}, {flagRepository, "owner//repo"},
		{flagServerURL, ""}, {flagServerURL, "https://user:secret@forge.example"}, {flagServerURL, "ssh://forge.example"}, {flagServerURL, "https://forge.example:99999"},
		{flagServerURL, "https://forge.example/path?query"}, {flagServerURL, "https://forge.example/%2e%2e"},
		{flagRef, ""}, {flagRef, "--upload-pack=bad"}, {flagRef, "main~1"}, {flagRef, "bad\nref"}, {flagRef, "bad*"},
		{"depth", "-1"}, {"object-format", "sha512"}, {flagFetchBase, "--bad"}, {flagFetchBase, "bad:base"},
		{flagSparse, "../escape"}, {flagSparse, "src,src"}, {flagSparse, "src/../other"}, {flagSparse, "src*"}, {flagSparse, "/absolute"},
		{flagOutputKey, "bad\nkey"}, {flagPath, "../escape"}, {flagPath, "/absolute"}, {"workspace", " \t"},
	} {
		t.Run(tc.flag+"/"+tc.value, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "new", "tree")
			probe := &checkoutCommandProbe{fetch: func(context.Context, string) (*provider.RepoMetadata, error) {
				return nil, errCheckoutCommandStop
			}}
			args := []string{commandCheckout, "--repository", "owner/repo", "--server-url", "https://forge.example", "--ref", "main", "--workspace", workspace, "--" + tc.flag, tc.value}
			err := probe.command(t, t.Context(), workspace).Run(t.Context(), args)
			require.Error(t, err)
			require.NotErrorIs(t, err, errCheckoutCommandStop)
			require.Empty(t, probe.events, "refuse even before dependency/sink construction")
			require.Empty(t, probe.sink.Keys())

			entries, readErr := os.ReadDir(root)
			require.NoError(t, readErr)
			require.Empty(t, entries, "preflight created directories")
		})
	}
}

func TestCheckoutCommand_WorkspaceRefusalBeforeMetadata(t *testing.T) { //nolint:paralleltest // CLI flag sources are process-global.
	checkoutCommandEnv(t)

	for _, state := range []string{"workspace-link", "workspace-link-parent", "ancestor-link", "dangling-workspace", "workspace-file", "ancestor-file", "git-link", "git-dangling", "git-file", "git-dir", "inspection-error"} {
		t.Run(state, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(outside, "canary"), []byte("outside data"), 0o600))
			workspace := filepath.Join(root, "workspace")

			target := workspace
			if strings.HasPrefix(state, "git-") {
				require.NoError(t, os.Mkdir(workspace, 0o755))
				target = filepath.Join(workspace, ".git")
			}

			switch state {
			case "workspace-link", "workspace-link-parent", "ancestor-link", "git-link":
				require.NoError(t, os.Symlink(outside, target))
			case "dangling-workspace":
				require.NoError(t, os.Symlink(filepath.Join(outside, "missing"), target))
			case "git-dangling":
				require.NoError(t, os.Symlink("missing", target))
			case "workspace-file", "ancestor-file", "git-file":
				require.NoError(t, os.WriteFile(target, []byte("caller data"), 0o600))
			case "git-dir":
				require.NoError(t, os.Mkdir(target, 0o755))
			case "inspection-error":
				workspace = filepath.Join(root, strings.Repeat("x", 256), "new")
			}

			if strings.HasPrefix(state, "ancestor-") {
				workspace = filepath.Join(workspace, "new", "tree")
			}

			if state == "workspace-link-parent" {
				workspace += "/../new"
			}

			probe := &checkoutCommandProbe{fetch: func(context.Context, string) (*provider.RepoMetadata, error) {
				return nil, errCheckoutCommandStop
			}}
			args := []string{commandCheckout, "--repository", "owner/repo", "--server-url", "https://forge.example", "--ref", "main", "--workspace", workspace}
			err := probe.command(t, t.Context(), workspace).Run(t.Context(), args)
			require.Error(t, err)
			require.NotErrorIs(t, err, errCheckoutCommandStop)
			require.Empty(t, probe.events)
			require.Empty(t, probe.sink.Keys())

			entries, readErr := os.ReadDir(outside)
			require.NoError(t, readErr)
			require.Len(t, entries, 1)

			body, readErr := os.ReadFile(filepath.Join(outside, "canary"))
			require.NoError(t, readErr)
			require.Equal(t, "outside data", string(body))

			entries, readErr = os.ReadDir(root)
			require.NoError(t, readErr)

			if state == "inspection-error" {
				require.Empty(t, entries)
			} else {
				require.Len(t, entries, 1)
			}
		})
	}
}

func TestCheckoutCommand_MetadataFormatAndRecheck(t *testing.T) { //nolint:paralleltest // runs actual CLI parsing with isolated environment sources.
	checkoutCommandEnv(t)

	for _, tc := range []struct {
		name, override, metadata, ref, mutation string
		wantFormat                              string
		wantErr                                 error
	}{
		{"provider-sha256", "", "sha256", strings.Repeat("a", 64), "", "sha256", errCheckoutCommandStop},
		{"provider-sha1", "", "sha1", strings.Repeat("b", 40), "", "sha1", errCheckoutCommandStop},
		{"provider-default", "", "", "main", "", "sha1", errCheckoutCommandStop},
		{"override", "sha256", "sha1", strings.Repeat("a", 64), "", "sha256", errCheckoutCommandStop},
		{"known-mismatch", "sha1", "sha256", strings.Repeat("a", 64), "", "", errs.ErrValidation},
		{"fetched-mismatch", "", "sha1", strings.Repeat("a", 64), "", "", errs.ErrValidation},
		{"default-mismatch", "", "", strings.Repeat("a", 64), "", "", errs.ErrValidation},
		{"unsupported-provider", "", "sha512", "main", "", "", errs.ErrUsage},
		{"metadata-error", "", "", "main", "error", "", errCheckoutCommandStop},
		{"metadata-cancelled", "", "", "main", "cancel", "", context.Canceled},
		{"recheck-git", "", "sha1", "main", "git", "", errs.ErrValidation},
		{"recheck-link", "", "sha1", "main", "link", "", errs.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			workspace := filepath.Join(root, "new", "tree")

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			probe := &checkoutCommandProbe{fetch: func(_ context.Context, repo string) (*provider.RepoMetadata, error) {
				require.Equal(t, "owner/repo", repo)

				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				require.Empty(t, entries, "metadata lookup must not create the workspace")

				switch tc.mutation {
				case "error":
					return nil, errCheckoutCommandStop
				case "cancel":
					cancel()

					return nil, context.Canceled
				case "git":
					require.NoError(t, os.MkdirAll(workspace, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(workspace, ".git"), []byte("concurrent caller marker"), 0o600))
				case "link":
					require.NoError(t, os.Symlink(outside, filepath.Join(root, "new")))
				}

				return &provider.RepoMetadata{ObjectFormat: tc.metadata}, nil
			}}
			args := []string{commandCheckout, "--repository", "owner/repo", "--server-url", "https://forge.example", "--ref", tc.ref, "--workspace", workspace, "--object-format", tc.override}
			err := probe.command(t, ctx, workspace).Run(ctx, args)
			require.ErrorIs(t, err, tc.wantErr)

			var expected []string
			if tc.name != "known-mismatch" {
				expected = []string{"deps"}
				if tc.override == "" {
					expected = append(expected, "metadata:owner/repo")
				}

				if tc.wantFormat != "" {
					expected = append(expected, "init:"+tc.wantFormat)
				}
			}

			require.Equal(t, expected, probe.events)
			require.Empty(t, probe.sink.Keys())

			if tc.wantFormat == "" && tc.mutation != "git" && tc.mutation != "link" {
				entries, readErr := os.ReadDir(root)
				require.NoError(t, readErr)
				require.Empty(t, entries)
			}

			if tc.mutation == "git" {
				body, readErr := os.ReadFile(filepath.Join(workspace, ".git"))
				require.NoError(t, readErr)
				require.Equal(t, "concurrent caller marker", string(body))
			}

			entries, readErr := os.ReadDir(outside)
			require.NoError(t, readErr)
			require.Empty(t, entries)
		})
	}
}

func TestCheckoutCommand_WorkspaceAuthority(t *testing.T) { //nolint:paralleltest // runner workspace and cwd are intentionally varied.
	checkoutCommandEnv(t)
	root := t.TempDir()
	runner, alias, explicit := filepath.Join(root, "runner"), filepath.Join(root, "alias"), filepath.Join(root, "operator-absolute")
	t.Setenv("FORGEJO_WORKSPACE", runner)
	t.Setenv("GITHUB_WORKSPACE", alias)

	for _, tc := range []struct{ name, workspace, path, want string }{
		{"explicit-outside-runner", explicit, "", explicit},
		{"contained-path-wins", explicit, "app/source", filepath.Join(runner, "app/source")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &checkoutCommandProbe{fetch: func(context.Context, string) (*provider.RepoMetadata, error) {
				_, err := os.Stat(tc.want)
				require.ErrorIs(t, err, os.ErrNotExist)

				return &provider.RepoMetadata{}, nil
			}}
			args := []string{commandCheckout, "--repository", "owner/repo", "--server-url", "https://forge.example", "--ref", "main", "--workspace", tc.workspace, "--path", tc.path}
			require.ErrorIs(t, probe.command(t, t.Context(), tc.want).Run(t.Context(), args), errCheckoutCommandStop)
			require.Equal(t, []string{"deps", "metadata:owner/repo", "init:sha1"}, probe.events)

			// A checkout that failed leaves nothing to clean up: the work ran in
			// a staging sibling and was removed, so the destination this run
			// would have created never appears.
			_, err := os.Stat(tc.want)
			require.ErrorIs(t, err, os.ErrNotExist, "a failed checkout must not publish a destination")

			entries, readErr := os.ReadDir(filepath.Dir(tc.want))
			require.NoError(t, readErr)
			require.Empty(t, entries, "the staging directory must be removed too")
		})
	}

	_, err := os.Stat(alias)
	require.ErrorIs(t, err, os.ErrNotExist)

	for _, linkedRunner := range []bool{false, true} {
		t.Run("linked-path/runner="+strconv.FormatBool(linkedRunner), func(t *testing.T) {
			parent, outside := t.TempDir(), t.TempDir()
			base, path := parent, "app/new"

			link := filepath.Join(parent, "app")
			if linkedRunner {
				base, path = filepath.Join(parent, "runner"), "app"
				link = base
			}

			require.NoError(t, os.Symlink(outside, link))
			t.Setenv("FORGEJO_WORKSPACE", base)

			probe := &checkoutCommandProbe{fetch: func(context.Context, string) (*provider.RepoMetadata, error) {
				return nil, errCheckoutCommandStop
			}}
			args := []string{commandCheckout, "--repository", "owner/repo", "--server-url", "https://forge.example", "--ref", "main", "--path", path}
			err := probe.command(t, t.Context(), filepath.Join(base, path)).Run(t.Context(), args)
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Empty(t, probe.events)
			require.Empty(t, probe.sink.Keys())

			entries, readErr := os.ReadDir(outside)
			require.NoError(t, readErr)
			require.Empty(t, entries)
		})
	}
}
