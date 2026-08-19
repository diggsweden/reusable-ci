// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestGenerateContainer_NoArtifactTypes_SingleGeneration(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	syft := &fakeSyft{}

	var out bytes.Buffer

	err := appsbom.GenerateContainer(context.Background(), syft, nil, &fakeGit{sha: "abc1234"}, &out, io.Discard, appsbom.GenerateContainerInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefName:     "v1.2.3",
		Repo:        "diggsweden/example",
		ImageName:   "ghcr.io/diggsweden/example",
		ImageDigest: "sha256:deadbeef",
	})
	if err != nil {
		t.Fatalf("GenerateContainer: %v\nstdout: %s", err, out.String())
	}
	// One syft call emits both SPDX and CycloneDX for the container target.
	if len(syft.calls) != 1 {
		t.Errorf("expected 1 syft call, got %d", len(syft.calls))
	}

	if got := len(syft.calls[0].outputs); got != 2 {
		t.Errorf("expected 2 output formats in the single call, got %d", got)
	}

	expectedImage := "ghcr.io/diggsweden/example@sha256:deadbeef"
	if syft.calls[0].target != expectedImage {
		t.Errorf("syft target = %q, want %q", syft.calls[0].target, expectedImage)
	}

	if !strings.Contains(out.String(), "No artifact dependencies") {
		t.Errorf("missing notice:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "✓ Container SBOM generation completed") {
		t.Errorf("missing completion line:\n%s", out.String())
	}
}

func TestGenerateContainer_VPrefixStripped(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.Chdir()

	syft := &fakeSyft{}
	if err := appsbom.GenerateContainer(context.Background(), syft, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateContainerInput{
		RefName:     "v9.8.7",
		Repo:        "org/repo",
		ImageName:   "img",
		ImageDigest: "sha256:abc",
	}); err != nil {
		t.Fatal(err)
	}
	// The SBOM filenames embed the version — verify the v was stripped.
	matches, _ := os.ReadDir(dir)
	found := false

	for _, e := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if strings.Contains(e.Name(), "-9.8.7-") {
			found = true
		}

		if strings.Contains(e.Name(), "-v9.8.7-") {
			t.Errorf("expected v-prefix to be stripped, got: %s", e.Name())
		}
	}

	if !found {
		t.Errorf("no file with 9.8.7 found, entries: %v", matches)
	}
}

// TestGenerateContainer_NamesEveryTypeButScansOnce covers a container with more
// than one declared artifact type: each is named in the output, and the image
// is scanned once.
func TestGenerateContainer_NamesEveryTypeButScansOnce(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	syft := &fakeSyft{}

	var out bytes.Buffer
	if err := appsbom.GenerateContainer(context.Background(), syft, nil, &fakeGit{}, &out, io.Discard, appsbom.GenerateContainerInput{
		ArtifactTypes: " maven , npm ",
		RefName:       "1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Repo:          "org/repo",
		ImageName:     "img",
		ImageDigest:   "sha256:abc",
	}); err != nil {
		t.Fatal(err)
	}
	// One scan, whatever the declared types. The analyzed-container layer
	// describes the image and is the same document either way, so scanning
	// once per type produced identical output twice -- the second run
	// overwriting the first, at the cost of a full image scan. Both types are
	// still named in the output below; only the scanning was duplicated.
	want := []syftCall{{
		target: "img@sha256:abc",
		outputs: map[string]string{
			"cyclonedx-json": "repo-1.0.0-analyzed-container-sbom.cyclonedx.json",
			"spdx-json":      "repo-1.0.0-analyzed-container-sbom.spdx.json",
		},
	}}
	if !reflect.DeepEqual(syft.calls, want) {
		t.Errorf("syft calls =\n%+v\nwant\n%+v", syft.calls, want)
	}

	for _, want := range []string{
		"Generating SBOM for artifact type: maven",
		"Generating SBOM for artifact type: npm",
		"✓ Container SBOM generation completed",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out missing %q:\n%s", want, out.String())
		}
	}
}

func TestGenerateContainer_RequiresAllFields(t *testing.T) {
	cases := []appsbom.GenerateContainerInput{
		{Repo: "r", ImageName: "i", ImageDigest: "d"},    // no RefName
		{RefName: "1", ImageName: "i", ImageDigest: "d"}, // no Repo
		{RefName: "1", Repo: "r", ImageDigest: "d"},      // no ImageName
		{RefName: "1", Repo: "r", ImageName: "i"},        // no ImageDigest
	}
	for i, c := range cases {
		if err := appsbom.GenerateContainer(context.Background(), &fakeSyft{}, nil, &fakeGit{}, io.Discard, io.Discard, c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}
