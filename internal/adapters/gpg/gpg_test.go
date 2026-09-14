//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	domaingpg "github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/gpgkey"
)

// TestImportKey_RoundTripWithRealGPG covers the on-disk-keyring import
// path that git's own `tag -s`/`commit -S` depends on. We verify success
// via ListKeygrips (the agent-integration entry point that actually
// matters downstream) rather than a separate fingerprint round-trip —
// the parsing-only `--list-secret-keys` shellout has been retired in
// favour of in-process metadata extraction via adapters/openpgp.
func TestImportKey_RoundTripWithRealGPG(t *testing.T) {
	k := gpgkey.New(t)
	armored := k.ArmoredPrivateKey()

	a := adaptergpg.New()
	ctx := context.Background()

	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	if err := a.ImportKey(ctx, []byte(armored)); err != nil {
		t.Fatalf("ImportKey: %v", err)
	}
	grips, err := a.ListKeygrips(ctx, k.Fingerprint)
	if err != nil || !strings.Contains(grips, "grp:") {
		t.Errorf("post-import ListKeygrips = %q, err = %v; want non-empty grp lines", grips, err)
	}
}

func TestListKeygrips_NonEmpty(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()

	out, err := a.ListKeygrips(context.Background(), k.Fingerprint)
	if err != nil {
		t.Fatalf("ListKeygrips: %v", err)
	}
	grips := domaingpg.ParseKeygrips(out)
	if len(grips) == 0 {
		t.Fatal("expected at least one keygrip")
	}
	for _, g := range grips {
		if len(g) != 40 || !isHex(g) {
			t.Errorf("keygrip %q is not a 40-char hex string", g)
		}
	}
}

func TestConfigureAgent_WritesConfFile(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()

	if err := a.ConfigureAgent(context.Background()); err != nil {
		t.Fatalf("ConfigureAgent: %v", err)
	}
	confPath := filepath.Join(k.GNUPGHOME, "gpg-agent.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read gpg-agent.conf: %v", err)
	}
	for _, want := range []string{"default-cache-ttl 21600", "max-cache-ttl 31536000", "allow-preset-passphrase"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("gpg-agent.conf missing %q\nfull:\n%s", want, data)
		}
	}
}

// TestDelete_RemovesTheKeyAndRepeatsHarmlessly covers both halves. The
// deletes return nothing and only report gpg's errors, so "it did not blow
// up" is not evidence of anything: what has to hold is that the key is
// actually gone afterwards, and that a second pass over an already-empty
// keyring still leaves it that way.
func TestDelete_RemovesTheKeyAndRepeatsHarmlessly(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()
	ctx := context.Background()

	// The key is there to begin with, or nothing below is a deletion.
	before, err := a.ListKeygrips(ctx, k.Fingerprint)
	if err != nil || len(domaingpg.ParseKeygrips(before)) == 0 {
		t.Fatalf("fixture key is not in the keyring: grips=%q err=%v", before, err)
	}

	// First round removes the throwaway key.
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	assertNoKeygrips(t, a, k.Fingerprint, "after the first delete")

	// Second round: keys are gone — the repeat must not resurrect or
	// error out, which for a void, best-effort call means the
	// keyring has to still be empty.
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	assertNoKeygrips(t, a, k.Fingerprint, "after the repeated delete")

	// Never-existed fingerprint.
	a.DeleteSecretKey(ctx, "0000000000000000000000000000000000000000")
	a.DeleteKey(ctx, "0000000000000000000000000000000000000000")

	assertNoKeygrips(t, a, k.Fingerprint, "after deleting an unrelated fingerprint")
}

// assertNoKeygrips fails unless the fingerprint resolves to no keygrips.
// gpg reports an unknown key as an error rather than an empty listing, so
// either outcome counts as absent.
func assertNoKeygrips(t *testing.T, a *adaptergpg.Adapter, fingerprint, when string) {
	t.Helper()

	out, err := a.ListKeygrips(context.Background(), fingerprint)
	if err == nil && len(domaingpg.ParseKeygrips(out)) != 0 {
		t.Errorf("%s the key is still in the keyring: %q", when, out)
	}
}

func isHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
