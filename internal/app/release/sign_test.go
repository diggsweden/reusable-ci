//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/gpgkey"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestSign_RequiresKeyID(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	err := apprelease.SignArtifacts(context.Background(), adaptergpg.New(), apprelease.SignInput{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "GPG_KEY_ID is required") {
		t.Errorf("err = %v, want GPG_KEY_ID-required", err)
	}
}

func TestSign_RealGPG_SignsChecksumsAndArtifacts(t *testing.T) {
	k := gpgkey.New(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	// Checksums file
	fsys.WriteFile("checksums.sha256", []byte("dummy hash  app.jar\n"))

	// release-artifacts/<name>.{jar,tgz}
	fsys.WriteFile(filepath.Join("release-artifacts", "app.jar"), []byte("jar content"))
	fsys.WriteFile(filepath.Join("release-artifacts", "frontend.tgz"), []byte("tgz"))
	// One that should NOT be signed (original-*.jar)
	fsys.WriteFile(filepath.Join("release-artifacts", "original-app.jar"), []byte("o"))

	err := apprelease.SignArtifacts(context.Background(), adaptergpg.New(), apprelease.SignInput{
		GPGKeyID: k.KeyID(),
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("SignArtifacts: %v", err)
	}

	// checksums.sha256.asc lives where the file is (cwd)
	if _, err := os.Stat("checksums.sha256.asc"); err != nil {
		t.Errorf("checksums.sha256.asc missing: %v", err)
	}
	// release artefact .asc files moved to cwd under basename
	for _, want := range []string{"app.jar.asc", "frontend.tgz.asc"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %q at cwd, got: %v", want, err)
		}
	}
	// original-*.jar must NOT have been signed
	if _, err := os.Stat("original-app.jar.asc"); err == nil {
		t.Errorf("original-app.jar.asc should not exist")
	}
}

func TestSign_NoChecksumsFileOrArtifactsDir_OK(t *testing.T) {
	k := gpgkey.New(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	// No checksums file, no release-artifacts dir — should succeed silently.
	err := apprelease.SignArtifacts(context.Background(), adaptergpg.New(), apprelease.SignInput{
		GPGKeyID: k.KeyID(),
	}, &bytes.Buffer{})
	if err != nil {
		t.Errorf("expected silent success, got: %v", err)
	}
}
