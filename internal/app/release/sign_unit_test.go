// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeSigner struct {
	signed []string
}

type silentSigner struct{}

func (silentSigner) Extensions() []string                   { return []string{".asc"} }
func (silentSigner) SignFile(context.Context, string) error { return nil }

func (f *fakeSigner) Extensions() []string { return []string{".asc"} }

func (f *fakeSigner) SignFile(_ context.Context, file string) error {
	f.signed = append(f.signed, file)

	return os.WriteFile(file+".asc", []byte("signature"), 0o600)
}

func TestSign_AttachedArtifactsAreSigned(t *testing.T) {
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

func TestSign_ExactFilesStayInPlace(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "app.sbom.json"), []byte("sbom"))
	fsys.Chdir()

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		Files: []string{filepath.Join("dist", "app.sbom.json"), filepath.Join("dist", "app.sbom.json")},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join("dist", "app.sbom.json.asc")); err != nil {
		t.Errorf("missing adjacent sidecar: %v", err)
	}

	if _, err := os.Stat("app.sbom.json.asc"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("exact file signing must not move sidecar to cwd, stat err = %v", err)
	}

	if got, want := len(signer.signed), 1; got != want {
		t.Fatalf("signed %d files, want deduped 1: %v", got, signer.signed)
	}
}

// TestSign_ExactFilesSignsEveryDistinctFile covers the loop. The dedupe
// test above passes the same path twice and asserts one signature, which
// is also what a loop that stops after its first iteration produces --
// the two claims are indistinguishable there.
//
// A release signs several files, and one left unsigned is not visible
// until a consumer tries to verify it.
func TestSign_ExactFilesSignsEveryDistinctFile(t *testing.T) {
	fsys := testfs.NewReal(t)
	for _, name := range []string{"app.sbom.json", "app.tar.gz", "app.jar"} {
		fsys.WriteFile(filepath.Join("dist", name), []byte(name))
	}

	fsys.Chdir()

	files := []string{
		filepath.Join("dist", "app.sbom.json"),
		filepath.Join("dist", "app.tar.gz"),
		filepath.Join("dist", "app.jar"),
	}

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		Files: files,
	}); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(signer.signed, files) {
		t.Errorf("signed = %v, want %v", signer.signed, files)
	}

	for _, f := range files {
		if _, err := os.Stat(f + ".asc"); err != nil {
			t.Errorf("missing sidecar for %s: %v", f, err)
		}
	}
}

// TestSign_ExactFileMissingRefusesBeforeSigningAnything pins the validation
// pass: one missing file refuses the full set before any sidecar is written.
func TestSign_ExactFileMissingRefusesBeforeSigningAnything(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "present.json"), []byte("x"))
	fsys.Chdir()

	signer := &fakeSigner{}

	err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		Files: []string{filepath.Join("dist", "present.json"), filepath.Join("dist", "missing.json")},
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	if got := signer.signed; len(got) != 0 {
		t.Errorf("signed = %v, want no mutation before validation completes", got)
	}
}

func TestSign_ExactFileRequiresRegularFile(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	err := apprelease.SignArtifacts(context.Background(), &fakeSigner{}, &bytes.Buffer{}, apprelease.SignInput{
		Files: []string{"dist/missing.json"},
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}
}

func TestSign_CompletePreflightPrecedesEverySignature(t *testing.T) {
	for _, late := range []string{"manifest section", "directory", "attachment"} {
		t.Run(late, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustWrite(t, "checksums.sha256", "checksums")
			mustWrite(t, "app.jar", "artifact")
			mustWrite(t, "checksums.sha256.asc", "old checksum signature")
			mustWrite(t, "app.jar.asc", "old artifact signature")

			in := apprelease.SignInput{Files: []string{"app.jar"}, SkipReleaseArtifactsDir: true}
			want := errs.ErrValidation

			switch late {
			case "manifest section":
				in.ManifestSections = []string{"evidence", "invalid-section"}
				want = errs.ErrUsage
			case "directory":
				mustWrite(t, "not-directory", "file")

				in.SkipReleaseArtifactsDir = false
				in.ReleaseArtifactsDir = "not-directory"
			case "attachment":
				in.AttachArtifacts = "app.jar,["
				want = filepath.ErrBadPattern
			}

			signer := &fakeSigner{}

			var out bytes.Buffer

			err := apprelease.SignArtifacts(t.Context(), signer, &out, in)
			if !errors.Is(err, want) {
				t.Errorf("err=%v, want %v", err, want)
			}

			if len(signer.signed) != 0 || out.Len() != 0 {
				t.Errorf("preflight caused effects: signed=%v output=%q", signer.signed, out.String())
			}

			if readFile(t, "checksums.sha256.asc") != "old checksum signature" || readFile(t, "app.jar.asc") != "old artifact signature" {
				t.Error("preflight changed existing sidecars")
			}
		})
	}
}

func TestSign_SkipDefaultTargets(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("checksums.sha256", []byte("checksum"))
	fsys.WriteFile(filepath.Join("release-artifacts", "app.jar"), []byte("jar"))
	fsys.Chdir()

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		SkipChecksumsFile:       true,
		SkipReleaseArtifactsDir: true,
	}); err != nil {
		t.Fatal(err)
	}

	if got := len(signer.signed); got != 0 {
		t.Fatalf("signed %d files, want 0: %v", got, signer.signed)
	}
}

func TestSign_ManifestSectionsAndChecksumFromManifest(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "app.tar.gz"), []byte("archive"))
	fsys.WriteFile(filepath.Join("dist", "app.sbom.json"), []byte("sbom"))

	archiveDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("archive")))
	sbomDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("sbom")))
	fsys.WriteFile(filepath.Join("dist", "app_checksums.txt"), []byte(archiveDigest+"  app.tar.gz\n"+sbomDigest+"  app.sbom.json\n"))
	fsys.WriteFile(filepath.Join("dist", "doctor.json"), []byte("evidence"))
	fsys.WriteFile(filepath.Join("dist", "release-files.json"), []byte(`{
  "version": 1,
  "assets": [
    {"path":"dist/app.tar.gz","name":"app.tar.gz","source":"goreleaser"},
    {"path":"dist/app_checksums.txt","name":"app_checksums.txt","source":"checksum"},
    {"path":"dist/app.sbom.json","name":"app.sbom.json","source":"sbom"},
    {"path":"dist/doctor.json","name":"doctor.json","source":"evidence"}
  ],
  "checksums": [{"path":"dist/app_checksums.txt","name":"app_checksums.txt","source":"checksum"}],
  "sboms": [{"path":"dist/app.sbom.json","name":"app.sbom.json","source":"sbom"}],
  "evidence": [{"path":"dist/doctor.json","name":"doctor.json","source":"evidence"}],
  "provenance": []
}
`))
	fsys.Chdir()

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{
		SkipReleaseArtifactsDir: true,
		ReleaseFilesManifest:    filepath.Join("dist", "release-files.json"),
		ReleaseFilesDistDir:     "dist",
		ChecksumsFromManifest:   true,
		ManifestSections:        []string{"sboms", "evidence"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		filepath.Join("dist", "app_checksums.txt"),
		filepath.Join("dist", "app.sbom.json"),
		filepath.Join("dist", "doctor.json"),
	} {
		if _, err := os.Stat(want + ".asc"); err != nil {
			t.Errorf("missing sidecar for %s: %v", want, err)
		}
	}

	if got, want := len(signer.signed), 3; got != want {
		t.Fatalf("signed %d files, want %d: %v", got, want, signer.signed)
	}
}

func TestSign_AssemblySignsDistroPackagesAsCanonicalAssets(t *testing.T) {
	// No t.Parallel: testfs.Real.Chdir mutates the process working directory.
	fsys := testfs.NewReal(t)

	packages := []string{
		filepath.Join("release-files", "assets", "app.deb"),
		filepath.Join("release-files", "assets", "app.rpm"),
		filepath.Join("release-files", "assets", "app.apk"),
	}
	for _, path := range packages {
		fsys.WriteFile(path, []byte("package"))
	}

	fsys.WriteFile("assembly.json", []byte(`{
  "version": 1,
  "assets": [
    {"path":"release-files/assets/app.deb","name":"app.deb"},
    {"path":"release-files/assets/app.rpm","name":"app.rpm"},
    {"path":"release-files/assets/app.apk","name":"app.apk"}
  ],
  "checksum_file":""
}`))
	fsys.Chdir()

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, io.Discard, apprelease.SignInput{AssemblyFile: "assembly.json"}); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(signer.signed, packages) {
		t.Errorf("signed = %v, want %v", signer.signed, packages)
	}

	for _, path := range packages {
		if _, err := os.Stat(path + ".asc"); err != nil {
			t.Errorf("missing canonical signature for %s: %v", path, err)
		}
	}
}

func TestSign_AssemblyFailsClosedWhenSignerProducesNoSidecar(t *testing.T) {
	// No t.Parallel: testfs.Real.Chdir mutates the process working directory.
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("release-files", "assets", "app.deb"), []byte("package"))
	fsys.WriteFile(filepath.Join("release-files", "assets", "app.deb.asc"), []byte("stale signature"))
	fsys.WriteFile("assembly.json", []byte(`{
  "version": 1,
  "assets": [{"path":"release-files/assets/app.deb","name":"app.deb"}],
  "checksum_file":""
}`))
	fsys.Chdir()

	err := apprelease.SignArtifacts(context.Background(), silentSigner{}, io.Discard, apprelease.SignInput{AssemblyFile: "assembly.json"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if _, statErr := os.Stat(filepath.Join("release-files", "assets", "app.deb.asc")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale sidecar survived failed signing: %v", statErr)
	}
}
