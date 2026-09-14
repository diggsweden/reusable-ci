// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	listed      string
	signed      []string
	imports     int
	lists       int
	sidecarMode string
}

func (f *fakeGPGPackageSigner) ImportKey(_ context.Context, keyData []byte) error {
	f.imports++

	if len(keyData) == 0 {
		return errEmptyKey
	}

	return nil
}

func (f *fakeGPGPackageSigner) ListSecretKeys(_ context.Context) (string, error) {
	f.lists++

	return f.listed, nil
}

func (f *fakeGPGPackageSigner) DetachedSign(_ context.Context, fingerprint, passphrase, inputPath, outputPath string) error {
	if fingerprint != "ABCDEF" || passphrase != "secret" {
		return errBadSigningInputs
	}

	f.signed = append(f.signed, inputPath)
	switch f.sidecarMode {
	case "silent":
		return nil
	case "empty":
		return os.WriteFile(outputPath, nil, 0o600)
	case "directory":
		return os.Mkdir(outputPath, 0o700)
	case "link":
		return os.Symlink(inputPath, outputPath)
	}

	return os.WriteFile(outputPath, []byte("sig"), 0o600)
}

// TestGPGSignPackages_SignsOnlyDistroPackages covers the selection as much as
// the signing: .deb, .rpm and .apk get a detached signature written beside
// them, and anything else in the directory is left alone.
//
// The signed files are compared as a sorted set. packageFiles globs one
// extension at a time, so the real order is grouped by extension rather than
// alphabetical, and nothing downstream depends on which package was signed
// first. The old fixture -- a.deb, b.rpm, c.apk -- could not tell those two
// rules apart, since both produce the same order.
func TestGPGSignPackages_SignsOnlyDistroPackages(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "z.deb"), []byte("deb"))
	fsys.WriteFile(filepath.Join("dist", "a.rpm"), []byte("rpm"))
	fsys.WriteFile(filepath.Join("dist", "m.apk"), []byte("apk"))
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

	signed := slices.Clone(gpg.signed)
	slices.Sort(signed)

	wantSigned := []string{"dist/a.rpm", "dist/m.apk", "dist/z.deb"}
	if !slices.Equal(signed, wantSigned) {
		t.Errorf("signed = %v, want %v", gpg.signed, wantSigned)
	}

	for _, want := range []string{"dist/z.deb.sig", "dist/a.rpm.sig", "dist/m.apk.sig"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	// The non-package file is in the fixture to be passed over. The signed
	// list above already catches it being handed to the signer; this looks at
	// the directory instead, so a signature left beside a file that should
	// not have one is reported as that rather than as a surprising count.
	if _, err := os.Stat("dist/ignored.txt.sig"); !os.IsNotExist(err) {
		t.Errorf("signed a file that is not a distro package: %v", err)
	}

	if out.String() != "GPG-signed 3 package(s)\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestGPGSignPackages_RequiresFreshNonEmptyRegularSidecars(t *testing.T) {
	for _, mode := range []string{"silent", "empty", "directory", "link"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			path := filepath.Join(dir, "app.deb")
			if err := os.WriteFile(path, []byte("package"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(path+".sig", []byte("stale signature"), 0o600); err != nil {
				t.Fatal(err)
			}

			signer := &fakeGPGPackageSigner{listed: "fpr:::::::::ABCDEF:", sidecarMode: mode}

			var out bytes.Buffer

			count, err := apprelease.GPGSignPackages(t.Context(), signer, &out, apprelease.GPGSignPackagesInput{Dir: dir, PrivateKey: testPGPKeyArmor, Fingerprint: "ABCDEF", Passphrase: "secret"})
			if !errors.Is(err, errs.ErrValidation) || count != 0 {
				t.Errorf("count=%d err=%v, want zero and ErrValidation", count, err)
			}

			if out.Len() != 0 {
				t.Error("reported success without a valid sidecar")
			}

			if len(signer.signed) != 1 {
				t.Error("fixture did not reach the signer")
			}

			if body, _ := os.ReadFile(path + ".sig"); string(body) == "stale signature" {
				t.Error("stale sidecar survived")
			}
		})
	}
}

// TestGPGSignPackages_DefaultsToTheDistDirectory pins the fallback taken when
// no directory is given. Every other test passes Dir explicitly, so the
// default was never exercised.
func TestGPGSignPackages_DefaultsToTheDistDirectory(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "a.deb"), []byte("deb"))
	fsys.Chdir()

	gpg := &fakeGPGPackageSigner{listed: "fpr:::::::::ABCDEF:"}

	count, err := apprelease.GPGSignPackages(context.Background(), gpg, &bytes.Buffer{}, apprelease.GPGSignPackagesInput{
		PrivateKey:  testPGPKeyArmor,
		Fingerprint: "ABCDEF",
		Passphrase:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 || !slices.Equal(gpg.signed, []string{"dist/a.deb"}) {
		t.Errorf("count = %d, signed = %v, want 1 and [dist/a.deb]", count, gpg.signed)
	}
}

func TestGPGSignPackages_FingerprintMismatch(t *testing.T) {
	t.Parallel()

	gpg := &fakeGPGPackageSigner{listed: "fpr:::::::::OTHER:"}

	_, err := apprelease.GPGSignPackages(context.Background(), gpg, &bytes.Buffer{}, apprelease.GPGSignPackagesInput{
		PrivateKey:  testPGPKeyArmor,
		Fingerprint: "ABCDEF",
		Passphrase:  "secret",
		Dir:         t.TempDir(),
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
