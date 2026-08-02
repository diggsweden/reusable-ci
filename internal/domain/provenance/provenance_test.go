// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/golden"
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

func TestBuild_Golden(t *testing.T) {
	t.Parallel()

	in := provenance.Input{
		Subjects: []provenance.Subject{
			{Name: "dist/app_linux_amd64.tar.gz", SHA256: strings.Repeat("a", 64)},
			{Name: "dist/checksums.txt", SHA256: strings.Repeat("b", 64)},
		},
		BuildType:    provenance.ReleaseBuildType,
		BuilderID:    "https://codeberg.org/itiquette/gommitlint/release.yml@v1.2.3",
		SourceURI:    "git+https://codeberg.org/itiquette/gommitlint",
		Ref:          "v1.2.3",
		InvocationID: "https://codeberg.org/itiquette/gommitlint/actions/runs/4242",
		StartedOn:    "2026-06-01T12:00:00Z",
		FinishedOn:   "2026-06-01T12:00:00Z",
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

	golden.Equal(t, "provenance_statement.json", body)
}

func TestPredicate_Golden(t *testing.T) {
	t.Parallel()

	// The container path: same builder/source/run vocabulary, predicate only
	// (cosign binds the image subject), with an image external parameter.
	body, err := provenance.Predicate(provenance.Input{
		BuildType:    provenance.ContainerBuildType,
		BuilderID:    "https://github.com/diggsweden/reusable-ci/v3/.github/workflows/publish-container.yml@refs/tags/v1.2.3",
		SourceURI:    "git+https://github.com/diggsweden/reusable-ci/v3",
		Ref:          "refs/tags/v1.2.3",
		ImageName:    "ghcr.io/diggsweden/app",
		InvocationID: "https://github.com/diggsweden/reusable-ci/v3/actions/runs/4242",
		StartedOn:    "2026-06-01T12:00:00Z",
		FinishedOn:   "2026-06-01T12:00:00Z",
		ResolvedDeps: []provenance.Dependency{
			provenance.SourceDependency("https://github.com/diggsweden/reusable-ci/v3", "refs/tags/v1.2.3", strings.Repeat("c", 40)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	golden.Equal(t, "provenance_predicate.json", body)
}

func TestPredicate_RequiresContext(t *testing.T) {
	t.Parallel()

	if _, err := provenance.Predicate(provenance.Input{}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty input should be usage error, got %v", err)
	}
}

// TestPredicate_BaseLineage proves base-image lineage is expressed in standard
// SLSA fields — flavor/base_input_id in externalParameters and the base image
// as a resolvedDependency annotated role=base-image — so no bespoke predicate
// type is needed.
func TestPredicate_BaseLineage(t *testing.T) {
	t.Parallel()

	body, err := provenance.Predicate(provenance.Input{
		BuildType:   provenance.ContainerBuildType,
		BuilderID:   "https://codeberg.org/itiquette/forgejo-ci/.forgejo/workflows/sign-container-images.yml@refs/tags/v1.2.3",
		SourceURI:   "git+https://codeberg.org/itiquette/forgejo-ci",
		Ref:         "refs/tags/v1.2.3",
		ImageName:   "codeberg.org/itiquette/ci-builder-rust",
		Flavor:      "rust",
		BaseInputID: strings.Repeat("a", 64),
		ResolvedDeps: []provenance.Dependency{
			provenance.SourceDependency("https://codeberg.org/itiquette/forgejo-ci", "refs/tags/v1.2.3", strings.Repeat("c", 40)),
			provenance.BaseImageDependency("docker.io/library/debian:trixie-slim", "sha256:"+strings.Repeat("b", 64)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		BuildDefinition struct {
			ExternalParameters   map[string]string `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI         string            `json:"uri"`
				Digest      map[string]string `json:"digest"`
				Annotations map[string]string `json:"annotations"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal predicate: %v", err)
	}

	ext := got.BuildDefinition.ExternalParameters
	if ext["flavor"] != "rust" {
		t.Errorf("externalParameters.flavor = %q, want rust", ext["flavor"])
	}

	if ext["base_input_id"] != strings.Repeat("a", 64) {
		t.Errorf("externalParameters.base_input_id = %q, want %q", ext["base_input_id"], strings.Repeat("a", 64))
	}

	var base *struct {
		URI         string            `json:"uri"`
		Digest      map[string]string `json:"digest"`
		Annotations map[string]string `json:"annotations"`
	}

	for i, dep := range got.BuildDefinition.ResolvedDependencies {
		if dep.Annotations["role"] == "base-image" {
			base = &got.BuildDefinition.ResolvedDependencies[i]
		}
	}

	if base == nil {
		t.Fatal("no resolvedDependency annotated role=base-image")
	}

	if base.URI != "docker.io/library/debian:trixie-slim" {
		t.Errorf("base uri = %q", base.URI)
	}

	if base.Digest["sha256"] != strings.Repeat("b", 64) {
		t.Errorf("base sha256 = %q, want %q (sha256: prefix must be stripped)", base.Digest["sha256"], strings.Repeat("b", 64))
	}

	// Pin the CLI's canonical base-lineage predicate bytes — the verify side of
	// forgejo-ci's golden-baseline-base-lineage-predicate. The two goldens are
	// NOT byte-identical (bash jq vs Go json.Marshal); each side is locked
	// independently and the flip is atomic, per the migration map's flip strategy.
	golden.Equal(t, "provenance_base_lineage_predicate.json", body)
}
