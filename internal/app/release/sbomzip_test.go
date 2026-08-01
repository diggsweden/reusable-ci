// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeSBOMSigner struct {
	signed []string
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
}

func TestCreateSBOMZip_BundlesAllLayers(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	dir := fsys.Root
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

	// Inspect the zip to check container SBOM is path-flattened.
	r, err := zip.OpenReader(filepath.Join(dir, res.ZipName)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = r.Close() }()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}

	sort.Strings(names)

	want := []string{
		"my-app-analyzed-container-sbom.spdx.json", // flattened (no sbom-artifacts/ prefix)
		"my-app-sbom.cyclonedx.json",
		"my-app-sbom.spdx.json",
	}
	if len(names) != len(want) {
		t.Fatalf("zip entries = %v, want %v", names, want)
	}

	for i, n := range names {
		if n != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, n, want[i])
		}
	}
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

func TestCreateSBOMZip_MixedFormatsAndTararchiveIncluded(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	dir := fsys.Root
	fsys.WriteFile("myapp-pom-sbom.spdx.json", []byte(`{"spdxVersion":"2.3"}`))
	fsys.WriteFile("myapp-analyzed-jar-sbom.cyclonedx.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.WriteFile("myapp-analyzed-tararchive-sbom.spdx.json", []byte(`{"spdx":"tararchive"}`))

	res, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName: "myapp", Version: "1.0.0",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	r, err := zip.OpenReader(filepath.Join(dir, res.ZipName)) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = r.Close() }()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}

	for _, want := range []string{"myapp-pom-sbom.spdx.json", "myapp-analyzed-jar-sbom.cyclonedx.json", "myapp-analyzed-tararchive-sbom.spdx.json"} {
		found := false

		for _, n := range names {
			if n == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("missing %q in zip entries %v", want, names)
		}
	}
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

	for _, testCase := range []struct {
		name string
		in   apprelease.SBOMZipInput
	}{
		{name: "flag false", in: apprelease.SBOMZipInput{ProjectName: "myapp", Version: "v1.2.3", SignArtifacts: false}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			signer := &fakeSBOMSigner{}

			res, err := apprelease.CreateSBOMZip(context.Background(), signer, testCase.in, &bytes.Buffer{})
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

			_ = os.Remove(filepath.Join(dir, "myapp-1.2.3-sboms.zip"))
		})
	}
}
