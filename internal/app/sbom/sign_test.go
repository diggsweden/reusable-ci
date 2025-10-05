// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
)

type fakeSBOMSigner struct {
	signed []string
	// failOn names the file whose signing fails.
	failOn string
}

var errSigningRefused = errors.New("signing refused")

func (f *fakeSBOMSigner) SignFile(_ context.Context, file string) error {
	f.signed = append(f.signed, filepath.Base(file))
	if filepath.Base(file) == f.failOn {
		return errSigningRefused
	}

	return nil
}

func TestSignAssembled_SignsOnlyCanonicalSBOMs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, f := range []string{
		"app-1.0-build-sbom.spdx.json",        // sign
		"app-1.0-build-sbom.cyclonedx.json",   // sign
		"app-1.0-build-sbom.spdx.json.bundle", // skip (a signature sidecar)
		"readme.txt",                          // skip (not an SBOM)
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	signer := &fakeSBOMSigner{}
	if err := appsbom.SignAssembled(context.Background(), signer, dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Counting alone cannot tell "signed both SBOMs" from "signed one SBOM
	// and the readme". findAssembledSBOMs sorts, so the order is fixed.
	want := []string{"app-1.0-build-sbom.cyclonedx.json", "app-1.0-build-sbom.spdx.json"}
	if !slices.Equal(signer.signed, want) {
		t.Errorf("signed = %v, want %v", signer.signed, want)
	}
}

func TestSignAssembled_NoSBOMsIsNoop(t *testing.T) {
	t.Parallel()

	signer := &fakeSBOMSigner{}

	var out bytes.Buffer
	if err := appsbom.SignAssembled(context.Background(), signer, t.TempDir(), &out); err != nil {
		t.Fatal(err)
	}

	if len(signer.signed) != 0 {
		t.Errorf("nothing should be signed, got %v", signer.signed)
	}

	// A no-op still has to say so: the step is idempotent, and an empty
	// directory must not look like a silent signing failure in the log.
	if !strings.Contains(out.String(), "nothing to sign") {
		t.Errorf("missing no-op notice:\n%s", out.String())
	}
}

// TestSignAssembled_ReportsExactlyWhatWasSigned covers a nested SBOM and a
// failure part-way through. Files are signed in sorted path order with one
// confirmation line each; when the second of three fails, the error names it
// with its cause, the third is never signed, and only the first is reported --
// its signature already exists and is not rolled back. Linked SBOMs are
// covered by the preflight guard test.
func TestSignAssembled_ReportsExactlyWhatWasSigned(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, dir string, names ...string) {
		t.Helper()

		for _, name := range names {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("failure part-way", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		write(t, dir, "a-sbom.spdx.json", "nested/b-sbom.cyclonedx.json", "z-sbom.spdx.json")

		signer := &fakeSBOMSigner{failOn: "b-sbom.cyclonedx.json"}

		var out bytes.Buffer

		err := appsbom.SignAssembled(context.Background(), signer, dir, &out)
		if !errors.Is(err, errSigningRefused) || !strings.Contains(err.Error(), filepath.Join(dir, "nested", "b-sbom.cyclonedx.json")) {
			t.Errorf("err = %v, want the refusal naming the nested file", err)
		}

		if want := []string{"a-sbom.spdx.json", "b-sbom.cyclonedx.json"}; !slices.Equal(signer.signed, want) {
			t.Errorf("signed = %v, want %v", signer.signed, want)
		}

		want := "🔏 Signing assembled SBOMs...\n   ✓ " + filepath.Join(dir, "a-sbom.spdx.json") + ".bundle\n"
		if out.String() != want {
			t.Errorf("stdout =\n%s\nwant\n%s", out.String(), want)
		}
	})
}
