// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"context"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

// TestImportKey_KeyArrivesOnStdinNotArgv pins the hardened behaviour:
// the armored key MUST flow into gpg via stdin, never via argv (where
// it would be visible to `ps`) and never via a tmpfile (where it
// would briefly land on disk). A stub gpg captures both surfaces; the
// test asserts the key is on one and absent from the other.
func TestImportKey_KeyArrivesOnStdinNotArgv(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", `cat > /dev/null`)

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

	// Argv must NOT carry the key path. Pre-hardening it was
	// `gpg --import --batch --yes /tmp/xyz/key.pgp` — post-hardening
	// no path argument exists. mockbinary's Args excludes argv[0],
	// so the recorded args are exactly the three flags.
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

	// Stdin must carry the full armored key. mockbinary's recorder
	// strips trailing newlines from the captured stdin, so we
	// compare with the same normalisation.
	if strings.TrimRight(inv.Stdin, "\n") != strings.TrimRight(armor, "\n") {
		t.Errorf("stdin (trimmed) = %q\nwant: %q", inv.Stdin, armor)
	}
}
