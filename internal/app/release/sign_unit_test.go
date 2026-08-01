// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeSigner struct {
	signed []string
}

func (f *fakeSigner) Extensions() []string { return []string{".asc"} }

func (f *fakeSigner) SignFile(_ context.Context, file string) error {
	f.signed = append(f.signed, file)

	return os.WriteFile(file+".asc", []byte("signature"), 0o600)
}

func TestSign_AttachedArtifactsAreSigned(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-artifacts", "app.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("release-artifacts", "linux-amd64", "demo-linux-amd64"), []byte("binary"))
	fsys.WriteFile(filepath.Join("release-artifacts", "darwin-arm64", "demo-darwin-arm64"), []byte("binary"))
	fsys.Chdir()

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		AttachArtifacts: "release-artifacts/*/demo-*",
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"app.jar.asc", "demo-linux-amd64.asc", "demo-darwin-arm64.asc"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	if got, want := len(signer.signed), 3; got != want {
		t.Fatalf("signed %d files, want %d: %v", got, want, signer.signed)
	}
}
