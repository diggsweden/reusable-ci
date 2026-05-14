// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestGenerateContainer_NoArtifactTypes_SingleGeneration(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	syft := &fakeSyft{}
	var stdout bytes.Buffer
	err := appsbom.GenerateContainer(context.Background(), syft, nil, &fakeGit{sha: "abc1234"}, &stdout, io.Discard, appsbom.GenerateContainerInput{
		RefName:     "v1.2.3",
		Repo:        "diggsweden/example",
		ImageName:   "ghcr.io/diggsweden/example",
		ImageDigest: "sha256:deadbeef",
	})
	if err != nil {
		t.Fatalf("GenerateContainer: %v\nstdout: %s", err, stdout.String())
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
	if !strings.Contains(stdout.String(), "No artifact dependencies") {
		t.Errorf("missing notice:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "✓ Container SBOM generation completed") {
		t.Errorf("missing completion line:\n%s", stdout.String())
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
	for _, e := range matches {
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

func TestGenerateContainer_LoopsPerArtifactType(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	syft := &fakeSyft{}
	var stdout bytes.Buffer
	if err := appsbom.GenerateContainer(context.Background(), syft, nil, &fakeGit{}, &stdout, io.Discard, appsbom.GenerateContainerInput{
		ArtifactTypes: " maven , npm ",
		RefName:       "1.0.0",
		Repo:          "org/repo",
		ImageName:     "img",
		ImageDigest:   "sha256:abc",
	}); err != nil {
		t.Fatal(err)
	}
	// 2 artifact-type iterations × 1 multi-output syft call each = 2 calls.
	if len(syft.calls) != 2 {
		t.Errorf("expected 2 syft calls (one per artifact type, each emitting both formats), got %d", len(syft.calls))
	}
	for i, c := range syft.calls {
		if got := len(c.outputs); got != 2 {
			t.Errorf("call %d: expected 2 output formats, got %d", i, got)
		}
	}
	for _, want := range []string{
		"Generating SBOM for artifact type: maven",
		"Generating SBOM for artifact type: npm",
		"✓ Container SBOM generation completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
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
