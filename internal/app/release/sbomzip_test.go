// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeSBOMSigner struct {
	signed []string
}

type sidecarSBOMSigner struct {
	advertised []string
	written    string
	content    []byte
}

func (s sidecarSBOMSigner) Extensions() []string { return s.advertised }

func (s sidecarSBOMSigner) SignFile(_ context.Context, file string) error {
	if s.written == "" {
		return nil
	}

	return os.WriteFile(file+s.written, s.content, 0o600)
}

func (f *fakeSBOMSigner) Extensions() []string { return []string{".asc"} }

func (f *fakeSBOMSigner) SignFile(_ context.Context, file string) error {
	f.signed = append(f.signed, file)

	return os.WriteFile(file+".asc", []byte("sig"), 0o644) //nolint:gosec // test fixture
}

func TestCreateSBOMZip_NoSBOMsIsNoOp(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName: "x", Version: "1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if res.ZipName != "" || res.EntryCount != 0 {
		t.Errorf("expected empty result for no-SBOMs, got %+v", res)
	}

	// And no archive was left behind. An empty zip published as a release
	// asset is worse than none: a consumer downloads it expecting SBOMs.
	entries, err := os.ReadDir(fsys.Root)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("a run with no SBOMs created %v", entries)
	}
}

// TestCreateSBOMZip_FlattensSBOMsFoundInDifferentPlaces varies where an SBOM
// was left: two beside the build and one in the container-scan output
// directory. All three go into the archive, and the nested one loses its
// directory prefix — a consumer unpacking the zip gets a flat set of SBOMs,
// not a sbom-artifacts/ tree.
func TestCreateSBOMZip_FlattensSBOMsFoundInDifferentPlaces(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("my-app-sbom.spdx.json", []byte(`{"spdx":1}`))
	fsys.WriteFile("my-app-sbom.cyclonedx.json", []byte(`{"cdx":1}`))
	fsys.WriteFile(filepath.Join("sbom-artifacts", "my-app-analyzed-container-sbom.spdx.json"), []byte(`{}`))

	res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName: "my-app", Version: "1.2.3",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if res.ZipName != "my-app-1.2.3-sboms.zip" {
		t.Errorf("zip name = %q", res.ZipName)
	}

	if res.EntryCount != 3 {
		t.Errorf("entries = %d, want 3", res.EntryCount)
	}

	assertZipEntries(t, res.ZipName, []string{
		"my-app-analyzed-container-sbom.spdx.json", // flattened: no sbom-artifacts/ prefix
		"my-app-sbom.cyclonedx.json",
		"my-app-sbom.spdx.json",
	})
}

func TestCreateSBOMZip_VersionedZipName(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		version     string
		fixture     string
		want        string
	}{
		{name: "strips_v_prefix", projectName: "x", version: "v2.0.0", fixture: "x-sbom.spdx.json", want: "x-2.0.0-sboms.zip"},
		{name: "keeps_prerelease_suffix", projectName: "myapp", version: "1.0.0-SNAPSHOT", fixture: "myapp-pom-sbom.spdx.json", want: "myapp-1.0.0-SNAPSHOT-sboms.zip"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			fsys.WriteFile(testCase.fixture, []byte(`{}`))

			res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
				ProjectName: testCase.projectName,
				Version:     testCase.version,
			}, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}

			if res.ZipName != testCase.want {
				t.Errorf("zip name = %q, want %q", res.ZipName, testCase.want)
			}
		})
	}
}

// TestCreateSBOMZip_RecognisesEverySBOMNamingConvention varies the filename
// instead of the location: the SPDX one a build tool writes from the POM, the
// CycloneDX one from analysing the built jar, and the SPDX one from analysing
// a tar archive. Each producer names its output differently and all three must
// be recognised as SBOMs.
func TestCreateSBOMZip_RecognisesEverySBOMNamingConvention(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{"spdxVersion":"2.3"}`))
	fsys.WriteFile("myapp-analyzed-jar-sbom.cyclonedx.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.WriteFile("myapp-analyzed-tararchive-sbom.spdx.json", []byte(`{"spdx":"tararchive"}`))

	res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName: "myapp", Version: "1.0.0",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	// Exactly these: the old check only looked for each wanted name and so
	// could not notice a fourth file being swept into a published archive.
	assertZipEntries(t, res.ZipName, []string{
		"myapp-pom-sbom.spdx.json",
		"myapp-analyzed-jar-sbom.cyclonedx.json",
		"myapp-analyzed-tararchive-sbom.spdx.json",
	})
}

// TestCreateSBOMZip_ScopesDiscoveryToTheProjectName is the negative control
// for the discovery scope: an SBOM another project (or a stray tool) left in
// the working directory or the container-SBOM dir must not be swept into this
// project's published, signed archive.
func TestCreateSBOMZip_ScopesDiscoveryToTheProjectName(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("my-app-sbom.spdx.json", []byte(`{}`))
	fsys.WriteFile("other-app-sbom.spdx.json", []byte(`{}`))
	fsys.WriteFile(filepath.Join("sbom-artifacts", "my-app-analyzed-container-sbom.spdx.json"), []byte(`{}`))
	fsys.WriteFile(filepath.Join("sbom-artifacts", "other-app-analyzed-container-sbom.spdx.json"), []byte(`{}`))

	res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName: "my-app", Version: "1.2.3",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	assertZipEntries(t, res.ZipName, []string{
		"my-app-sbom.spdx.json",
		"my-app-analyzed-container-sbom.spdx.json",
	})
}

func TestCreateSBOMZip_SigningRequestedCreatesAsc(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	dir := fsys.Root
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{}`))

	signer := &fakeSBOMSigner{}

	var out bytes.Buffer

	res, err := apprelease.CreateSBOMZip(context.Background(), signer, apprelease.SBOMZipInput{
		ProjectName:   "myapp",
		Version:       "v1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		SignArtifacts: true,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Signed {
		t.Fatal("expected Signed=true")
	}

	if len(signer.signed) != 1 || signer.signed[0] != "myapp-1.2.3-sboms.zip" {
		t.Errorf("signed = %v", signer.signed)
	}

	if _, err := os.Stat(filepath.Join(dir, "myapp-1.2.3-sboms.zip.asc")); err != nil {
		t.Errorf("zip signature missing: %v", err)
	}

	if !strings.Contains(out.String(), "Signed SBOM ZIP") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCreateSBOMZip_SigningSkippedWithoutFlag(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	dir := fsys.Root
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{}`))

	signer := &fakeSBOMSigner{}

	res, err := apprelease.CreateSBOMZip(context.Background(), signer, apprelease.SBOMZipInput{
		ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: false,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if res.Signed {
		t.Fatalf("expected Signed=false")
	}

	if len(signer.signed) != 0 {
		t.Errorf("signer should not be called, got %v", signer.signed)
	}

	if _, err := os.Stat(filepath.Join(dir, "myapp-1.2.3-sboms.zip.asc")); !os.IsNotExist(err) {
		t.Errorf("zip signature should not exist: %v", err)
	}
}

func TestCreateSBOMZip_SigningRejectsMissingEmptyAndWrongSidecars(t *testing.T) {
	tests := []struct {
		name   string
		signer sidecarSBOMSigner
	}{
		{name: "missing", signer: sidecarSBOMSigner{advertised: []string{".asc"}}},
		{name: "empty", signer: sidecarSBOMSigner{advertised: []string{".asc"}, written: ".asc"}},
		{name: "wrong extension", signer: sidecarSBOMSigner{advertised: []string{".sigstore.json"}, written: ".asc", content: []byte("sig")}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{}`))

			_, err := apprelease.CreateSBOMZip(context.Background(), testCase.signer, apprelease.SBOMZipInput{
				ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: true,
			}, &bytes.Buffer{})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestCreateSBOMZip_SigningRemovesStaleSidecarBeforeBackend(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{}`))
	fsys.WriteFile("myapp-1.2.3-sboms.zip.asc", []byte("stale"))

	_, err := apprelease.CreateSBOMZip(context.Background(), sidecarSBOMSigner{advertised: []string{".asc"}}, apprelease.SBOMZipInput{
		ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: true,
	}, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}

	if _, statErr := os.Stat("myapp-1.2.3-sboms.zip.asc"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale sidecar survived signing attempt: %v", statErr)
	}
}

func TestCreateSBOMZip_SigningUsesAdvertisedExtension(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{}`))

	var out bytes.Buffer

	res, err := apprelease.CreateSBOMZip(context.Background(), sidecarSBOMSigner{
		advertised: []string{".sigstore.json"}, written: ".sigstore.json", content: []byte("bundle"),
	}, apprelease.SBOMZipInput{
		ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: true,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Signed {
		t.Fatal("expected Signed=true")
	}

	if _, statErr := os.Stat("myapp-1.2.3-sboms.zip.sigstore.json"); statErr != nil {
		t.Fatalf("advertised sidecar missing: %v", statErr)
	}

	if strings.Contains(out.String(), ".asc") {
		t.Fatalf("signing output assumes an OpenPGP sidecar: %q", out.String())
	}
}
