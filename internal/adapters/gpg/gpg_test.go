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

func TestDelete_IsIdempotent(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()
	ctx := context.Background()

	// First round removes the throwaway key.
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	// Second round: keys are gone — must not panic / error-propagate.
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	// Never-existed fingerprint.
	a.DeleteSecretKey(ctx, "0000000000000000000000000000000000000000")
	a.DeleteKey(ctx, "0000000000000000000000000000000000000000")
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

// TestImportKey_ErrorNeverEchoesInput: import stdin is key material by
// definition, and gpg echoes input fragments in diagnostics ("invalid
// armor header: <line>") that carry no private-key marker — so the
// import error must suppress gpg's output wholesale, not rely on
// marker-based redaction. Black-box twin lives in the companion
// testsuite (TestSecurity_GPGStderrDoesNotEchoInputKey).
func TestImportKey_ErrorNeverEchoesInput(t *testing.T) {
	t.Parallel()

	payload := "this-is-not-base64-armor-payload"
	err := adaptergpg.New().ImportKey(context.Background(), []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\n"+payload+"\n-----END PGP PRIVATE KEY BLOCK-----\n"))
	if err == nil {
		t.Fatal("import of garbage armor must fail")
	}

	if strings.Contains(err.Error(), payload) {
		t.Fatalf("import error echoed input key material:\n%s", err)
	}
}
