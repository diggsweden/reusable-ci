// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"context"
	"os"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestImportKey_KeyArrivesOnStdinNotArgv pins the hardened behaviour:
// the armored key MUST flow into gpg via stdin, never via argv (where
// it would be visible to `ps`) and never via a tmpfile (where it
// would briefly land on disk). A stub gpg captures both surfaces; the
// test asserts the key is on one and absent from the other.
//
// The no-tmpfile half of that claim is checked rather than asserted in prose.
// It holds today by construction — os/exec pipes a non-*os.File Stdin, so a
// strings.Reader never reaches the filesystem — but the regression it warns
// about is a refactor to os.CreateTemp, and that writes into TMPDIR. Pointing
// TMPDIR at an owned directory and requiring it to stay empty is the seam
// that would catch it. Nothing here polls for a file that exists only during
// the call: a staged key would have to be created and removed, and an empty
// directory afterwards is not proof it was never used, so what is asserted is
// the durable part — a leftover — plus the argv and stdin surfaces above.
func TestImportKey_KeyArrivesOnStdinNotArgv(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", `cat > /dev/null`)

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	a := &adaptergpg.Adapter{GPGBin: bins.Path("gpg")}

	const armor = "-----BEGIN PGP PRIVATE KEY BLOCK-----\nfake-payload\n-----END PGP PRIVATE KEY BLOCK-----\n"
	if err := a.ImportKey(context.Background(), []byte(armor)); err != nil {
		t.Fatalf("ImportKey: %v", err)
	}

	invs := bins.Invocations("gpg")
	if len(invs) != 1 {
		t.Fatalf("expected 1 gpg invocation, got %d", len(invs))
	}

	inv := invs[0]

	// Argv must not carry a key path. mockbinary's Args excludes argv[0], so
	// the recorded args are exactly the three flags.
	expectedArgs := []string{"--import", "--batch", "--yes"}
	if len(inv.Args) != len(expectedArgs) {
		t.Errorf("unexpected argv length %d: %v (key path leaked into argv?)", len(inv.Args), inv.Args)
	} else {
		for i, want := range expectedArgs {
			if inv.Args[i] != want {
				t.Errorf("argv[%d] = %q, want %q", i, inv.Args[i], want)
			}
		}
	}

	// Argv must not contain a path that points at a key file.
	for _, a := range inv.Args {
		if strings.HasSuffix(a, ".pgp") || strings.HasSuffix(a, ".key") || strings.HasSuffix(a, ".asc") {
			t.Errorf("argv leaks a key path: %q", a)
		}
	}

	// Stdin must carry the full armored key, byte for byte. This used to
	// compare both sides with trailing newlines trimmed, because the
	// recorder's "$(cat)" dropped them; it no longer does, and comparing
	// trimmed hid the one thing an armor is sensitive to. gpg refuses a
	// private-key block whose END line has no terminating newline, so a
	// caller that dropped it would fail in CI while the test that exists to
	// pin what gpg receives stayed green.
	if inv.Stdin != armor {
		t.Errorf("stdin = %q\nwant: %q", inv.Stdin, armor)
	}

	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read TMPDIR: %v", err)
	}

	if len(left) != 0 {
		names := make([]string, 0, len(left))
		for _, e := range left {
			names = append(names, e.Name())
		}

		t.Errorf("the import left %v in TMPDIR; key material must not be staged on disk", names)
	}
}
