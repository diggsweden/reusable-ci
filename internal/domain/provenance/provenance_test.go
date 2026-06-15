// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/internal/testutil/golden"
)

func TestParseChecksums(t *testing.T) {
	t.Parallel()

	in := strings.NewReader(
		"DEADBEEF" + strings.Repeat("0", 56) + "  dist/app_linux_amd64.tar.gz\n" +
			"\n" + // blank line skipped
			strings.Repeat("a", 64) + "  dist/checksums.txt\n")

	subjects, err := provenance.ParseChecksums(in)
	if err != nil {
		t.Fatal(err)
	}

	if len(subjects) != 2 {
		t.Fatalf("got %d subjects, want 2", len(subjects))
	}

	if subjects[0].Name != "dist/app_linux_amd64.tar.gz" {
		t.Errorf("name = %q", subjects[0].Name)
	}

	// Digest must be lower-cased.
	if subjects[0].SHA256 != "deadbeef"+strings.Repeat("0", 56) {
		t.Errorf("sha = %q (should be lower-cased)", subjects[0].SHA256)
	}
}

func TestParseChecksums_Errors(t *testing.T) {
	t.Parallel()

	if _, err := provenance.ParseChecksums(strings.NewReader("not a checksum line\n")); err == nil {
		t.Error("expected error on malformed line")
	}

	if _, err := provenance.ParseChecksums(strings.NewReader("\n   \n")); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty input should be usage error, got %v", err)
	}
}

func TestParseGoSum(t *testing.T) {
	t.Parallel()

	in := strings.NewReader(
		"github.com/foo/bar v1.2.3 h1:abc123=\n" +
			"github.com/foo/bar v1.2.3/go.mod h1:shouldskip=\n" +
			"example.com/m v0.1.0+incompatible h1:def456=\n")

	deps, err := provenance.ParseGoSum(in)
	if err != nil {
		t.Fatal(err)
	}

	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2 (go.mod line skipped)", len(deps))
	}

	if deps[0].URI != "pkg:golang/github.com/foo/bar@v1.2.3" || deps[0].DigestType != "gomod_h1" || deps[0].Digest != "abc123" {
		t.Errorf("dep0 = %+v", deps[0])
	}

	// "+incompatible" must be percent-encoded in the purl.
	if deps[1].URI != "pkg:golang/example.com/m@v0.1.0%2Bincompatible" {
		t.Errorf("dep1 uri = %q (should escape +)", deps[1].URI)
	}
}

func TestBuild_RequiresSubjectsAndContext(t *testing.T) {
	t.Parallel()

	if _, err := provenance.Build(provenance.Input{}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty input should be usage error, got %v", err)
	}

	// Subjects present but identifying context missing.
	_, err := provenance.Build(provenance.Input{Subjects: []provenance.Subject{{Name: "x", SHA256: "y"}}})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing context should be usage error, got %v", err)
	}
}

func TestBuild_GoldenForgejo(t *testing.T) {
	t.Parallel()

	in := provenance.Input{
		Subjects: []provenance.Subject{
			{Name: "dist/app_linux_amd64.tar.gz", SHA256: strings.Repeat("a", 64)},
			{Name: "dist/checksums.txt", SHA256: strings.Repeat("b", 64)},
		},
		RepositoryURL: "https://codeberg.org/itiquette/gommitlint",
		Ref:           "v1.2.3",
		WorkflowFile:  "release.yml",
		BuilderID:     "https://codeberg.org/itiquette/gommitlint/.forgejo/workflows/release.yml@v1.2.3",
		InvocationID:  "https://codeberg.org/itiquette/gommitlint/actions/runs/4242",
		StartedOn:     "2026-06-01T12:00:00Z",
		FinishedOn:    "2026-06-01T12:00:00Z",
		Profile: provenance.Profile{
			BuildType:         "https://forgejo.org/actions/buildtypes/workflow/v1",
			WorkflowDirPrefix: ".forgejo/workflows/",
			RunnerLabel:       "forgejo-actions",
		},
		ResolvedDeps: []provenance.Dependency{
			provenance.SourceDependency("https://codeberg.org/itiquette/gommitlint", "v1.2.3", strings.Repeat("c", 40)),
			{URI: "pkg:golang/github.com/foo/bar@v1.2.3", DigestType: "gomod_h1", Digest: "abc123"},
		},
	}

	stmt, err := provenance.Build(in)
	if err != nil {
		t.Fatal(err)
	}

	body, err := stmt.JSON()
	if err != nil {
		t.Fatal(err)
	}

	golden.Equal(t, "forgejo_provenance.json", body)
}
