//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpgkey_test

import (
	"encoding/hex"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/gpgkey"
)

func TestNew_GeneratesValidKey(t *testing.T) {
	k := gpgkey.New(t)

	if got := len(k.Fingerprint); got != 40 {
		t.Errorf("Fingerprint length = %d, want 40 (hex chars): %q", got, k.Fingerprint)
	}
	if k.Name == "" || k.Email == "" {
		t.Errorf("Name/Email empty: %q / %q", k.Name, k.Email)
	}
	if got := len(k.KeyID()); got != 16 {
		t.Errorf("KeyID length = %d, want 16: %q", got, k.KeyID())
	}
	if !strings.HasSuffix(k.Fingerprint, k.KeyID()) {
		t.Errorf("KeyID %q is not a suffix of fingerprint %q", k.KeyID(), k.Fingerprint)
	}
}

func TestArmoredKeys_ContainHeaders(t *testing.T) {
	k := gpgkey.New(t)
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "private",
			body: k.ArmoredPrivateKey(),
			want: []string{
				"-----BEGIN PGP PRIVATE KEY BLOCK-----",
				"-----END PGP PRIVATE KEY BLOCK-----",
			},
		},
		{
			name: "public",
			body: k.ArmoredPublicKey(),
			want: []string{
				"-----BEGIN PGP PUBLIC KEY BLOCK-----",
				"-----END PGP PUBLIC KEY BLOCK-----",
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			for _, want := range testCase.want {
				if !strings.Contains(testCase.body, want) {
					t.Errorf("armored %s key missing %q", testCase.name, want)
				}
			}
		})
	}
}

func TestKeys_AreIsolated(t *testing.T) {
	a := gpgkey.New(t)
	b := gpgkey.New(t)
	if a.Fingerprint == b.Fingerprint {
		t.Errorf("two New() calls returned the same fingerprint: %q", a.Fingerprint)
	}
	if a.GNUPGHOME == b.GNUPGHOME {
		t.Errorf("two New() calls returned the same GNUPGHOME: %q", a.GNUPGHOME)
	}
}

// TestArmoredKeys_ParseAsTheKeyTheyClaimToBe upgrades the header check above.
// Substring assertions on BEGIN/END lines are satisfied by two header lines
// with nothing between them, and by a public block mislabelled as a private
// one — both of which would make every downstream signing fixture fail in a
// way that points at the code under test rather than at the fixture.
//
// The parser used here is the one production uses on this same armor, so what
// this proves is that the fixture is readable by the code it feeds, not merely
// that gpg emitted something.
func TestArmoredKeys_ParseAsTheKeyTheyClaimToBe(t *testing.T) {
	k := gpgkey.New(t)

	for name, armor := range map[string]string{
		"private": k.ArmoredPrivateKey(),
		"public":  k.ArmoredPublicKey(),
	} {
		t.Run(name, func(t *testing.T) {
			md, err := openpgp.ReadMetadata([]byte(armor))
			if err != nil {
				t.Fatalf("parse armored %s key: %v", name, err)
			}

			if md.Fingerprint != k.Fingerprint {
				t.Errorf("parsed fingerprint = %q, want %q", md.Fingerprint, k.Fingerprint)
			}

			if md.KeyID != k.KeyID() {
				t.Errorf("parsed key ID = %q, want %q", md.KeyID, k.KeyID())
			}

			if md.Name != k.Name || md.Email != k.Email {
				t.Errorf("parsed identity = %q <%s>, want %q <%s>", md.Name, md.Email, k.Name, k.Email)
			}
		})
	}

	// The private armor must carry the secret half. ReadMetadata reads only
	// the public material, so it cannot tell the two blocks apart; the signer
	// constructor refuses a public-only armor by name.
	signer, err := openpgp.NewSignerFromArmor([]byte(k.ArmoredPrivateKey()), "")
	if err != nil {
		t.Fatalf("the private armor is not usable for signing: %v", err)
	}

	if got := signer.Fingerprint(); got != k.Fingerprint {
		t.Errorf("signer fingerprint = %q, want %q", got, k.Fingerprint)
	}

	if _, err := openpgp.NewSignerFromArmor([]byte(k.ArmoredPublicKey()), ""); err == nil {
		t.Error("the public armor was accepted as signing material, so the two exports are not distinct")
	}
}

// TestFingerprint_IsUppercaseHex pins the shape callers rely on. The length
// check above is satisfied by any forty characters, and the fingerprint is
// passed straight to gpg as a key selector and compared against git config
// values, where case and alphabet both matter.
func TestFingerprint_IsUppercaseHex(t *testing.T) {
	k := gpgkey.New(t)

	raw, err := hex.DecodeString(k.Fingerprint)
	if err != nil {
		t.Fatalf("fingerprint %q is not hex: %v", k.Fingerprint, err)
	}

	// 20 bytes: an OpenPGP v4 fingerprint is a SHA-1 over the public-key
	// packet, and gpg prints it as 40 hex characters.
	if len(raw) != 20 {
		t.Errorf("fingerprint decodes to %d bytes, want 20", len(raw))
	}

	if k.Fingerprint != strings.ToUpper(k.Fingerprint) {
		t.Errorf("fingerprint %q is not uppercase", k.Fingerprint)
	}
}

// TestKeys_AreIsolated_AcrossKeyrings is the isolation claim the sibling test
// names but does not check. Distinct fingerprints and distinct directory paths
// are both true of two keys sharing one keyring; what matters for a fixture
// that runs beside other tests is that neither key can be found through the
// other's GNUPGHOME.
func TestKeys_AreIsolated_AcrossKeyrings(t *testing.T) {
	a := gpgkey.New(t)
	b := gpgkey.New(t)

	for _, pair := range []struct {
		home    string
		present string
		absent  string
	}{
		{home: a.GNUPGHOME, present: a.Fingerprint, absent: b.Fingerprint},
		{home: b.GNUPGHOME, present: b.Fingerprint, absent: a.Fingerprint},
	} {
		listed := listSecretFingerprints(t, pair.home)
		if !slices.Contains(listed, pair.present) {
			t.Errorf("%s does not hold its own key %s: %v", pair.home, pair.present, listed)
		}

		if slices.Contains(listed, pair.absent) {
			t.Errorf("%s can see the other keyring's key %s: %v", pair.home, pair.absent, listed)
		}
	}
}

// listSecretFingerprints reads the secret-key fingerprints in a GNUPGHOME
// directly, rather than through the fixture, so the fixture cannot be the
// source of its own isolation evidence.
func listSecretFingerprints(t *testing.T, homedir string) []string {
	t.Helper()

	//nolint:gosec // test infra; homedir is t.TempDir()-based.
	out, err := exec.CommandContext(t.Context(), "gpg", "--homedir", homedir,
		"--batch", "--with-colons", "--list-secret-keys").Output()
	if err != nil {
		t.Fatalf("list-secret-keys in %q: %v", homedir, err)
	}

	var fingerprints []string

	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Split(line, ":"); strings.HasPrefix(line, "fpr:") && len(fields) >= 10 {
			fingerprints = append(fingerprints, fields[9])
		}
	}

	return fingerprints
}

// TestGpgkey_RequiresItsHostDependencies names what this fixture needs from
// the host, in one place, the way mockbinary's host-dependency guard does.
//
// The audit item that prompted this asked for a "clean missing-tool skip"
// instead. That is the wrong trade here and the skip is deliberately not
// added: this package is behind the `integration` build tag, which is the
// opt-in that says the host has the integration toolchain. A skip would turn
// a missing gpg into a green run in which none of the signing, cleanup or
// import paths executed — the exact failure the tag exists to prevent. What
// was actually missing is a failure that names the tool once rather than
// fifteen times through whichever key generation happened to run first.
func TestGpgkey_RequiresItsHostDependencies(t *testing.T) {
	t.Parallel()

	for _, dep := range []struct {
		bin  string
		what string
	}{
		{bin: "gpg", what: "keys are generated and exported with --quick-generate-key and --export-secret-keys"},
		{bin: "gpgconf", what: "cleanup stops the per-homedir gpg-agent with --kill gpg-agent"},
		{bin: "chmod", what: "GNUPGHOME must be 0700 before gpg will use it"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			t.Errorf("gpgkey needs %s on PATH: %s.\n"+
				"Without it every test using this fixture fails without naming %s.",
				dep.bin, dep.what, dep.bin)
		}
	}
}
