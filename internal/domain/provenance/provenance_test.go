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

func TestParseChecksums_ReadsBinaryModeAndSkipsBlankLines(t *testing.T) {
	t.Parallel()

	in := strings.NewReader(
		"DEADBEEF" + strings.Repeat("0", 56) + "  dist/app_linux_amd64.tar.gz\n" +
			strings.Repeat("c", 64) + "  *dist/binary-mode.tar.gz\r\n" +
			"\n" + // blank line skipped
			strings.Repeat("a", 64) + "  dist/checksums.txt\n")

	subjects, err := provenance.ParseChecksums(in)
	if err != nil {
		t.Fatal(err)
	}

	if len(subjects) != 3 {
		t.Fatalf("got %d subjects, want 3", len(subjects))
	}

	if subjects[0].Name != "dist/app_linux_amd64.tar.gz" {
		t.Errorf("name = %q", subjects[0].Name)
	}

	// Digest must be lower-cased.
	if subjects[0].SHA256 != "deadbeef"+strings.Repeat("0", 56) {
		t.Errorf("sha = %q (should be lower-cased)", subjects[0].SHA256)
	}

	if subjects[1].Name != "dist/binary-mode.tar.gz" {
		t.Errorf("binary-mode name = %q (should strip coreutils '*' marker and CRLF)", subjects[1].Name)
	}
}

func TestParseChecksums_Errors(t *testing.T) {
	t.Parallel()

	// A checksums file that does not parse is a rule failure about supplied
	// data, distinct from the ErrUsage below for "you gave me nothing".
	_, err := provenance.ParseChecksums(strings.NewReader("not a checksum line\n"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("malformed line: err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "not a checksum line") {
		t.Errorf("err = %v, want it to quote the offending line", err)
	}

	if _, err := provenance.ParseChecksums(strings.NewReader("\n   \n")); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty input should be usage error, got %v", err)
	}
}

func TestParseGoSum_SkipsGoModHashes(t *testing.T) {
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
		BuilderID:    "https://github.com/diggsweden/reusable-ci/.github/workflows/publish-container.yml@refs/tags/v1.2.3",
		SourceURI:    "git+https://github.com/diggsweden/reusable-ci",
		Ref:          "refs/tags/v1.2.3",
		ImageName:    "ghcr.io/diggsweden/app",
		InvocationID: "https://github.com/diggsweden/reusable-ci/actions/runs/4242",
		StartedOn:    "2026-06-01T12:00:00Z",
		FinishedOn:   "2026-06-01T12:00:00Z",
		ResolvedDeps: []provenance.Dependency{
			provenance.SourceDependency("https://github.com/diggsweden/reusable-ci", "refs/tags/v1.2.3", strings.Repeat("c", 40)),
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

	// Pin the generic reusable-ci base-lineage predicate bytes (the
	// `container attest --base-*` path; forgejo-ci's retired shipped
	// compatibility shape now lives on only in signed attestations).
	golden.Equal(t, "provenance_base_lineage_predicate.json", body)
}

// TestBuild_WorkflowProfileRequiresEachField: the workflow profile replaces
// source and ref with the calling workflow's coordinates, so each of the
// three has to be present. A statement with an empty repository or path
// would verify against nothing.
func TestBuild_WorkflowProfileRequiresEachField(t *testing.T) {
	t.Parallel()

	complete := func() provenance.Input {
		return provenance.Input{
			Subjects:  []provenance.Subject{{Name: "a", SHA256: strings.Repeat("a", 64)}},
			BuildType: provenance.ReleaseBuildType, BuilderID: "https://forge.example/b", SourceURI: "git+https://forge.example/r",
			Workflow: &provenance.WorkflowExternalParameters{Ref: "refs/tags/v1", Repository: "org/repo", Path: ".github/workflows/release.yml"},
		}
	}

	if _, err := provenance.Build(complete()); err != nil {
		t.Fatalf("control: %v", err)
	}

	for name, blank := range map[string]func(*provenance.WorkflowExternalParameters){
		"Workflow.Ref":        func(w *provenance.WorkflowExternalParameters) { w.Ref = "" },
		"Workflow.Repository": func(w *provenance.WorkflowExternalParameters) { w.Repository = "" },
		"Workflow.Path":       func(w *provenance.WorkflowExternalParameters) { w.Path = "" },
	} {
		in := complete()
		blank(in.Workflow)

		if _, err := provenance.Build(in); !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s blank: err = %v, want ErrUsage naming the field", name, err)
		}
	}
}

// TestPredicate_OmitsAbsentRefAndEmptyInternalParameters: an absent ref is
// left out of externalParameters rather than emitted as "", and an internal
// parameter with no value is dropped, so a verifier never compares against an
// empty string that means "unknown".
func TestPredicate_OmitsAbsentRefAndEmptyInternalParameters(t *testing.T) {
	t.Parallel()

	body, err := provenance.Predicate(provenance.Input{
		BuildType: provenance.ReleaseBuildType, BuilderID: "https://forge.example/b", SourceURI: "git+https://forge.example/r",
		InternalParameters: map[string]string{"runner": "ubuntu", "unset": ""},
	})
	if err != nil {
		t.Fatal(err)
	}

	var predicate struct {
		BuildDefinition struct {
			ExternalParameters map[string]any    `json:"externalParameters"`
			InternalParameters map[string]string `json:"internalParameters"`
		} `json:"buildDefinition"`
	}
	if err := json.Unmarshal(body, &predicate); err != nil {
		t.Fatal(err)
	}

	if _, present := predicate.BuildDefinition.ExternalParameters["ref"]; present {
		t.Errorf("an absent ref was emitted: %v", predicate.BuildDefinition.ExternalParameters)
	}

	if got := predicate.BuildDefinition.InternalParameters; len(got) != 1 || got["runner"] != "ubuntu" {
		t.Errorf("internalParameters = %v, want only the set one", got)
	}
}

// TestEnvelopes_AcceptsEveryShapeCosignPrints covers the three shapes:
// one object, an array, and JSON Lines, which is what cosign
// verify-attestation prints for an image carrying several attestations. The
// lines shape used to be refused as malformed, so a release image with both a
// CycloneDX and a SLSA attestation could not be verified or pruned.
func TestEnvelopes_AcceptsEveryShapeCosignPrints(t *testing.T) {
	t.Parallel()

	one := `{"payloadType":"application/vnd.in-toto+json","payload":"e30=","signatures":[]}`
	two := `{"payloadType":"application/vnd.in-toto+json","payload":"e30K","signatures":[]}`

	for name, testCase := range map[string]struct {
		body string
		want int
	}{
		"object": {body: one, want: 1},
		"array":  {body: "[" + one + "," + two + "]", want: 2},
		"lines":  {body: one + "\n" + two + "\n", want: 2},
	} {
		envelopes, err := provenance.Envelopes([]byte(testCase.body))
		if err != nil || len(envelopes) != testCase.want {
			t.Errorf("%s: envelopes = %d, %v; want %d", name, len(envelopes), err, testCase.want)
		}
	}

	for name, body := range map[string]string{"empty": "", "blank": "\n", "truncated": one[:20], "trailing garbage": one + "\nnot json"} {
		if _, err := provenance.Envelopes([]byte(body)); !errors.Is(err, errs.ErrMalformedInput) {
			t.Errorf("%s: err = %v, want ErrMalformedInput", name, err)
		}
	}

	// An envelope without a payload is skipped by the payload accessor rather
	// than decoded as an empty statement.
	if _, ok := provenance.EnvelopePayload(map[string]any{"payload": ""}); ok {
		t.Error("an empty payload was reported as present")
	}

	if _, ok := provenance.EnvelopePayload("not an envelope"); ok {
		t.Error("a non-object was reported as an envelope")
	}
}

// TestParseGoSum_SkipsShortLines states the handling of lines with fewer than
// three fields: they are skipped, not refused, matching the jq pipeline this
// replaced. A truncated final line therefore drops silently.
func TestParseGoSum_SkipsShortLines(t *testing.T) {
	t.Parallel()

	deps, err := provenance.ParseGoSum(strings.NewReader("github.com/a/b v1.0.0 h1:abc=\ngithub.com/c/d v2.0.0\n\ngithub.com/e/f v3.0.0 h1:def=\n"))
	if err != nil {
		t.Fatal(err)
	}

	if len(deps) != 2 || deps[0].URI != "pkg:golang/github.com/a/b@v1.0.0" || deps[1].URI != "pkg:golang/github.com/e/f@v3.0.0" {
		t.Errorf("deps = %+v, want the two complete lines", deps)
	}
}
