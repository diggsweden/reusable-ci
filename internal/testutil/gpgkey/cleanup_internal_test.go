// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpgkey

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestCleanup_KillsTheAgentAfterTheTestContextEnds registers a key's cleanup on
// a subtest and lets the subtest finish, so the cleanup runs after that test's
// context is cancelled, as it does for every real key. A fake gpgconf on PATH
// must record exactly one agent shutdown for the key's own home. With the
// test's context the command was never started.
func TestCleanup_KillsTheAgentAfterTheTestContextEnds(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpgconf", "exit 0")

	home := t.TempDir()

	t.Run("key", func(t *testing.T) {
		key := &Key{t: t, GNUPGHOME: home}
		t.Cleanup(key.cleanup)
	})

	calls := bins.Invocations("gpgconf")
	if len(calls) != 1 || !slices.Equal(calls[0].Args, []string{"--homedir", home, "--kill", "gpg-agent"}) {
		t.Fatalf("gpgconf invocations = %+v, want one agent shutdown for %s", calls, home)
	}
}
