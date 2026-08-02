// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

var (
	errEmptyKey         = errors.New("empty key")
	errBadSigningInputs = errors.New("bad signing inputs")
)

// testPGPKeyArmor is an empty PGP armor test fixture, not a real key.
//
//nolint:gosec // G101 false positive: empty armor test fixture, not a real credential.
const testPGPKeyArmor = "-----BEGIN PGP PRIVATE KEY BLOCK-----\n-----END PGP PRIVATE KEY BLOCK-----"

type fakeGPGPackageSigner struct {
	listed string
	signed []string
}

func (f *fakeGPGPackageSigner) ImportKey(_ context.Context, keyData []byte) error {
	if len(keyData) == 0 {
		return errEmptyKey
	}

	return nil
}

func (f *fakeGPGPackageSigner) ListSecretKeys(_ context.Context) (string, error) {
	return f.listed, nil
}

func (f *fakeGPGPackageSigner) DetachedSign(_ context.Context, fingerprint, passphrase, inputPath, outputPath string) error {
	if fingerprint != "ABCDEF" || passphrase != "secret" {
		return errBadSigningInputs
	}

	f.signed = append(f.signed, inputPath)

	return os.WriteFile(outputPath, []byte("sig"), 0o600)
}

func TestGPGSignPackages_SignsDistroPackagesInPlace(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "a.deb"), []byte("deb"))
	fsys.WriteFile(filepath.Join("dist", "b.rpm"), []byte("rpm"))
	fsys.WriteFile(filepath.Join("dist", "c.apk"), []byte("apk"))
	fsys.WriteFile(filepath.Join("dist", "ignored.txt"), []byte("ignored"))
	fsys.Chdir()

	gpg := &fakeGPGPackageSigner{listed: "fpr:::::::::ABCDEF:"}

	var out bytes.Buffer

	count, err := apprelease.GPGSignPackages(context.Background(), gpg, &out, apprelease.GPGSignPackagesInput{
		PrivateKey:  testPGPKeyArmor,
		Fingerprint: "ABCDEF",
		Passphrase:  "secret",
		Dir:         "dist",
	})
	if err != nil {
		t.Fatal(err)
	}

	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	for _, want := range []string{"dist/a.deb.sig", "dist/b.rpm.sig", "dist/c.apk.sig"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	if got := gpg.signed; len(got) != 3 || got[0] != "dist/a.deb" || got[1] != "dist/b.rpm" || got[2] != "dist/c.apk" {
		t.Fatalf("signed order = %v", got)
	}

	if out.String() != "GPG-signed 3 package(s)\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestGPGSignPackages_FingerprintMismatch(t *testing.T) {
	t.Parallel()

	gpg := &fakeGPGPackageSigner{listed: "fpr:::::::::OTHER:"}

	_, err := apprelease.GPGSignPackages(context.Background(), gpg, &bytes.Buffer{}, apprelease.GPGSignPackagesInput{
		PrivateKey:  testPGPKeyArmor,
		Fingerprint: "ABCDEF",
		Passphrase:  "secret",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestGPGSignPackages_RequiresInputs(t *testing.T) {
	t.Parallel()

	_, err := apprelease.GPGSignPackages(context.Background(), &fakeGPGPackageSigner{}, &bytes.Buffer{}, apprelease.GPGSignPackagesInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}
