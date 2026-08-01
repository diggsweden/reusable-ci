// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
)

type fakeSBOMSigner struct{ signed []string }

func (f *fakeSBOMSigner) SignFile(_ context.Context, file string) error {
	f.signed = append(f.signed, filepath.Base(file))

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
	if err := appsbom.SignAssembled(context.Background(), signer, dir, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	if len(signer.signed) != 2 {
		t.Errorf("signed = %v, want the 2 canonical SBOMs only", signer.signed)
	}
}

func TestSignAssembled_NoSBOMsIsNoop(t *testing.T) {
	t.Parallel()

	signer := &fakeSBOMSigner{}
	if err := appsbom.SignAssembled(context.Background(), signer, t.TempDir(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	if len(signer.signed) != 0 {
		t.Errorf("nothing should be signed, got %v", signer.signed)
	}
}
