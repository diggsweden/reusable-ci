// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestConfigureAgent_EnforcesModesAndReloadsOnce starts from a GNUPGHOME that
// is group-readable and holds a world-readable gpg-agent.conf that is a
// symlink to another file. ConfigureAgent must leave the directory at 0700 and
// a regular 0600 file with exactly the canonical configuration, leave the
// symlink's target untouched, and ask the agent to reload exactly once.
//
// Not parallel: sets GNUPGHOME and uses mock binaries.
func TestConfigureAgent_EnforcesModesAndReloadsOnce(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg-connect-agent", "exit 0")

	home := filepath.Join(t.TempDir(), "gnupg")
	require.NoError(t, os.Mkdir(home, 0o750))
	require.NoError(t, os.Chmod(home, 0o750)) //nolint:gosec // an owned temp directory made deliberately too open.

	elsewhere := filepath.Join(t.TempDir(), "elsewhere.conf")
	require.NoError(t, os.WriteFile(elsewhere, []byte("keep\n"), 0o644)) //nolint:gosec // an owned fixture made deliberately world-readable.
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(home, "gpg-agent.conf")))
	t.Setenv("GNUPGHOME", home)

	adapter := &adaptergpg.Adapter{AgentBin: bins.Path("gpg-connect-agent")}
	require.NoError(t, adapter.ConfigureAgent(context.Background()))

	dir, err := os.Stat(home)
	require.NoError(t, err)
	require.Equal(t, fs.ModeDir|0o700, dir.Mode())

	conf, err := os.Lstat(filepath.Join(home, "gpg-agent.conf"))
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), conf.Mode())

	body, err := os.ReadFile(filepath.Join(home, "gpg-agent.conf"))
	require.NoError(t, err)
	require.Equal(t, domaingpg.AgentConfig, string(body))

	kept, err := os.ReadFile(elsewhere)
	require.NoError(t, err)
	require.Equal(t, "keep\n", string(kept))

	calls := bins.All()
	require.Len(t, calls, 1)
	require.Equal(t, []string{"reloadagent", "/bye"}, calls[0].Args)
}

// TestConfigureAgent_FailuresAreDistinct: a GNUPGHOME that is a regular file
// fails at the directory before any file is written or the agent is called,
// and a refused reload is reported as the reload after the file was written.
//
// Not parallel: sets GNUPGHOME and uses mock binaries.
func TestConfigureAgent_FailuresAreDistinct(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg-connect-agent", `printf 'no agent' >&2; exit 2`)

	adapter := &adaptergpg.Adapter{AgentBin: bins.Path("gpg-connect-agent")}

	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o600))
	t.Setenv("GNUPGHOME", notDir)

	err := adapter.ConfigureAgent(context.Background())
	require.ErrorContains(t, err, "mkdir GNUPGHOME")
	require.Empty(t, bins.All())

	home := filepath.Join(t.TempDir(), "gnupg")
	t.Setenv("GNUPGHOME", home)

	err = adapter.ConfigureAgent(context.Background())
	require.ErrorContains(t, err, "reload gpg-agent")
	require.ErrorContains(t, err, "no agent")
	require.FileExists(t, filepath.Join(home, "gpg-agent.conf"))
	require.Len(t, bins.All(), 1)
}

// TestDelete_EachHalfReportsItsOwnFailure: gpg refusing only the secret-key
// deletion logs only the secret-key warning, and refusing only the public one
// logs only the public warning; the other deletion still runs.
//
// Not parallel: swaps the default logger and uses mock binaries.
func TestDelete_EachHalfReportsItsOwnFailure(t *testing.T) {
	const fingerprint = "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"

	for flag, want := range map[string]string{
		"--delete-secret-keys": "failed to delete GPG secret key",
		"--delete-keys":        "failed to delete GPG public key",
	} {
		bins := mockbinary.New(t)
		bins.Add("gpg", `for arg in "$@"; do [ "$arg" = "`+flag+`" ] && { printf refused >&2; exit 2; }; done; exit 0`)

		var logs bytes.Buffer

		previous := slog.Default()

		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

		adapter := &adaptergpg.Adapter{GPGBin: bins.Path("gpg")}
		adapter.DeleteSecretKey(context.Background(), fingerprint)
		adapter.DeleteKey(context.Background(), fingerprint)
		slog.SetDefault(previous)

		lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
		require.Len(t, lines, 1, logs.String())
		require.Contains(t, lines[0], want)
		require.Contains(t, lines[0], fingerprint)

		flags := make([]string, 0, 2)
		for _, call := range bins.All() {
			flags = append(flags, call.Args[2])
		}

		require.True(t, slices.Equal([]string{"--delete-secret-keys", "--delete-keys"}, flags), flags)
	}
}

// TestImportKey_ErrorNeverEchoesInput runs the import against a fake gpg that
// copies every byte of its input into its diagnostics and fails, the worst
// case for an import whose input is key material. The error names the failed
// import and its class, and carries no line of the input; the fake received
// the whole key on stdin and nothing on argv.
//
// It used to run the real gpg with the caller's HOME and GNUPGHOME, touching
// the developer's keyring, and could only pass when that gpg happened not to
// echo the payload.
//
// Not parallel: uses mock binaries on PATH.
func TestImportKey_ErrorNeverEchoesInput(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", "cat >&2; exit 2")

	payload := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nsecret-armor-payload-line\n-----END PGP PRIVATE KEY BLOCK-----\n" //nolint:gosec // synthetic armor with no key material.

	err := (&adaptergpg.Adapter{GPGBin: bins.Path("gpg")}).ImportKey(context.Background(), []byte(payload))
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Contains(t, err.Error(), "diagnostics suppressed")
	require.NotContains(t, err.Error(), "secret-armor-payload-line")
	require.NotContains(t, err.Error(), "PRIVATE KEY")

	calls := bins.Invocations("gpg")
	require.Len(t, calls, 1)
	require.Equal(t, []string{"--import", "--batch", "--yes"}, calls[0].Args)
	require.Equal(t, payload, calls[0].Stdin)
}
