//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpg_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	domaingpg "github.com/diggsweden/reusable-ci/internal/domain/gpg"
	"github.com/diggsweden/reusable-ci/internal/testutil/gpgkey"
)

func TestImportKey_RoundTripWithRealGPG(t *testing.T) {
	// gpgkey.New sets GNUPGHOME to a temp dir + generates a throwaway key.
	// We export it, wipe the keyring, then re-import via the adapter and
	// verify the fingerprint matches.
	k := gpgkey.New(t)
	armored := k.ArmoredPrivateKey()

	a := adaptergpg.New()
	ctx := context.Background()

	// Wipe so ImportKey actually has work to do.
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)

	if err := a.ImportKey(ctx, []byte(armored)); err != nil {
		t.Fatalf("ImportKey: %v", err)
	}

	fpr, err := a.FirstFingerprint(ctx)
	if err != nil {
		t.Fatalf("FirstFingerprint: %v", err)
	}
	if fpr != k.Fingerprint {
		t.Errorf("imported fingerprint = %q, want %q", fpr, k.Fingerprint)
	}
}

func TestListSecretKey_ParseableColons(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()

	out, err := a.ListSecretKey(context.Background(), k.Fingerprint)
	if err != nil {
		t.Fatalf("ListSecretKey: %v", err)
	}
	md := domaingpg.ParseColonsOutput(out)
	if md.Fingerprint != k.Fingerprint {
		t.Errorf("Fingerprint = %q, want %q", md.Fingerprint, k.Fingerprint)
	}
	if md.Email != k.Email {
		t.Errorf("Email = %q, want %q", md.Email, k.Email)
	}
	if md.Name != k.Name {
		t.Errorf("Name = %q, want %q", md.Name, k.Name)
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
