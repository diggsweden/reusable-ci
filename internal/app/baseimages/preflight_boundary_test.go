// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestFreshnessBoundary_CompleteValidationAndResolution(t *testing.T) {
	t.Parallel()

	const repo = "registry.example/owner/base"

	digest := "sha256:" + strings.Repeat("a", 64)
	checks := []FreshnessCheck{
		{Name: "FIRST", PinnedRef: repo + "@" + digest, SourceTag: repo + ":first"},
		{Name: "SECOND", PinnedRef: repo + "@" + digest, SourceTag: repo + ":second"},
		{Name: "THIRD", PinnedRef: repo + "@" + digest, SourceTag: repo + ":third"},
	}

	for _, kind := range []string{"valid", "label", "pinned", "source", "returned"} {
		t.Run(kind, func(t *testing.T) {
			items := slices.Clone(checks)

			switch kind {
			case "label":
				items[1].Name = "unsafe\x1b[2J"
			case "pinned":
				items[1].PinnedRef = repo + "@sha256:bad"
			case "source":
				items[1].SourceTag = "owner/base:tag"
			}

			body, err := json.Marshal(items)
			if err != nil {
				t.Fatal(err)
			}

			registry := &fakeBaseImageRegistry{digests: map[string]string{repo + ":first": digest, repo + ":second": "sha256:" + strings.Repeat("b", 64), repo + ":third": "sha256:" + strings.Repeat("c", 64)}}
			if kind == "returned" {
				registry.digests[repo+":second"] = "sha256:bad\noutput"
			}

			sink := fakeoutputsink.New(t)

			var out, stderr bytes.Buffer

			err = CheckFreshness(t.Context(), registry, sink, &out, &stderr, CheckFreshnessInput{ChecksJSON: string(body)})
			if kind == "valid" {
				if err != nil || sink.Single("stale-count") != "2" || !slices.Equal(registry.resolved, []string{repo + ":first", repo + ":second", repo + ":third"}) || !strings.Contains(out.String(), "FIRST digest is current") || !strings.Contains(stderr.String(), "SECOND digest is stale") || !strings.Contains(stderr.String(), "THIRD digest is stale") {
					t.Fatalf("err=%v calls=%v output=%s/%s", err, registry.resolved, &out, &stderr)
				}
			} else {
				if err == nil || len(sink.Keys()) != 0 || out.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("err=%v outputs=%v text=%s/%s", err, sink.Keys(), &out, &stderr)
				}

				if kind != "returned" && len(registry.resolved) != 0 {
					t.Fatalf("resolver reached invalid input: %v", registry.resolved)
				}
			}
		})
	}
}

type boundaryVerifier struct {
	*fakeBaseImageVerifier
	refs   []string
	cancel context.CancelFunc
	refuse string
}

func (f *boundaryVerifier) VerifyImage(_ context.Context, in domaincontainer.ImageVerifyRequest, _ io.Writer) error {
	f.refs = append(f.refs, in.ImageRef)
	if in.ImageRef == f.refuse {
		f.cancel()

		return context.Canceled
	}

	return nil
}

func TestPromotionBoundary_AllEntriesBeforeCopy(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"valid", "late metadata", "late digest", "duplicate", "late final", "late evidence"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			first := promoteImage()
			first.CandidateRef = first.Ref
			first.CandidateTag = promoteRepo + ":candidate-rust"
			second := first
			second.Flavor = "go"
			second.Tag = promoteRepo + ":go-final"
			second.Ref = promoteRepo + "@sha256:" + strings.Repeat("b", 64)
			second.CandidateRef = second.Ref
			second.CandidateTag = promoteRepo + ":candidate-go"
			registry := &fakeBaseImageRegistry{digests: map[string]string{first.CandidateTag: promoteDigest, second.CandidateTag: "sha256:" + strings.Repeat("b", 64)}}
			verifier := &boundaryVerifier{fakeBaseImageVerifier: &fakeBaseImageVerifier{payload: baseLineagePayload(t,
				lineageFields{source: "https://codeberg.org/itiquette/base", workflow: ".forgejo/workflows/base-images.yml", flavor: "rust", baseInputID: promoteInput},
				lineageFields{source: "https://codeberg.org/itiquette/base", workflow: ".forgejo/workflows/base-images.yml", flavor: "go", baseInputID: promoteInput})}, cancel: cancel}

			switch kind {
			case "late metadata":
				second.Flavor = "invalid/flavor"
			case "late digest":
				second.CandidateRef = first.Ref
			case "duplicate":
				second.Tag = first.Tag
			case "late final":
				registry.digests[second.Tag] = promoteDigest
			case "late evidence":
				verifier.refuse = second.Ref
			}

			var out bytes.Buffer

			_, err := PromoteBaseImages(ctx, registry, verifier, &out, promoteInputFor(first, second))
			if kind == "valid" {
				if err != nil || !slices.Equal(registry.copied, []string{first.Ref + "->" + first.Tag, second.Ref + "->" + second.Tag}) || !slices.Equal(verifier.refs, []string{first.Ref, second.Ref}) {
					t.Fatalf("err=%v copies=%v verify=%v", err, registry.copied, verifier.refs)
				}
			} else {
				if err == nil || len(registry.copied) != 0 || out.Len() != 0 {
					t.Fatalf("err=%v copies=%v output=%s", err, registry.copied, &out)
				}

				if (kind == "late metadata" || kind == "late digest" || kind == "duplicate") && (len(registry.resolved) != 0 || len(verifier.refs) != 0) {
					t.Fatal("local refusal reached dependencies")
				}
			}
		})
	}
}

func TestCleanupBoundary_UncertainSweepPreservesCandidates(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"missing", "unavailable", "mismatch", "architecture unknown", "architecture unavailable"} {
		t.Run(kind, func(t *testing.T) {
			version := "staging-" + pruneIDKept + "-rust"
			final := pruneRepo + ":" + pruneIDKept + "-rust"
			registry := &fakeBaseImageRegistry{digests: map[string]string{pruneRepo + ":" + version: promoteDigest}, resolveErr: map[string]error{}}
			in := BaseImageCleanupStagingInput{ExpectedRepository: pruneRepo}

			switch kind {
			case "unavailable":
				registry.resolveErr[final] = errs.ErrDependencyUnavailable
			case "mismatch":
				registry.digests[final] = "sha256:" + strings.Repeat("b", 64)
			case "architecture unknown":
				version += "-amd64"
			case "architecture unavailable":
				version += "-amd64"
				in.BaseInputs = []BaseInput{{Flavor: "rust", ContentID: pruneIDKept, BaseInputID: pruneIDKept}}
				registry.resolveErr[final] = errs.ErrDependencyUnavailable
			}

			cleaner := &fakeBaseImagePackageAPI{versions: []string{version}}

			err := CleanupStagingBaseImages(t.Context(), registry, cleaner, io.Discard, in)
			if len(cleaner.deleted) != 0 {
				t.Fatalf("deleted=%v", cleaner.deleted)
			}

			if strings.Contains(kind, "unavailable") && !errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Fatalf("err=%v", err)
			}

			if kind == "mismatch" && !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestPruneLineageBoundary_RequiresBoundSLSA(t *testing.T) { //nolint:gocognit,forcetypeassert // mutate our known fixture at each independent binding, then exercise the public prune path.
	t.Parallel()

	for _, kind := range []string{"valid distinct hashes", "wrong predicate", "foreign repository", "malformed ref", "unbound digest", "unbound input", "multiple statements", "late release ref"} {
		t.Run(kind, func(t *testing.T) {
			payload := prunePayload(t, pruneIDKept, false)

			var envelope map[string]string
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Fatal(err)
			}

			decoded, err := base64.StdEncoding.DecodeString(envelope["payload"])
			if err != nil {
				t.Fatal(err)
			}

			var statement map[string]any
			if decodeErr := json.Unmarshal(decoded, &statement); decodeErr != nil {
				t.Fatal(decodeErr)
			}

			predicate, _ := statement["predicate"].(map[string]any)
			definition, _ := predicate["buildDefinition"].(map[string]any)
			external, _ := definition["externalParameters"].(map[string]any)
			base, _ := external["base"].(map[string]any)
			dependencies, _ := definition["resolvedDependencies"].([]any)
			dependency, _ := dependencies[0].(map[string]any)

			switch kind {
			case "wrong predicate":
				statement["predicateType"] = "https://example.invalid/not-slsa"
			case "foreign repository":
				base["ref"] = "registry.example/other/base@sha256:" + strings.Repeat("a", 64)
				dependency["uri"] = "oci://" + base["ref"].(string) //nolint:forcetypeassert // string assigned on the preceding line.
			case "malformed ref":
				base["ref"] = pruneRepo + ":latest"
			case "unbound digest":
				dependency["digest"] = map[string]any{"sha256": strings.Repeat("b", 64)}
			case "unbound input":
				dependency["annotations"] = map[string]any{"base_input_id": pruneIDOther}
			}

			decoded, err = json.Marshal(statement)
			if err != nil {
				t.Fatal(err)
			}

			envelope["payload"] = base64.StdEncoding.EncodeToString(decoded)
			if kind == "multiple statements" {
				payload, err = json.Marshal([]any{envelope, envelope})
			} else {
				payload, err = json.Marshal(envelope)
			}

			if err != nil {
				t.Fatal(err)
			}

			pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
			verifier := &fakePruneVerifier{payloads: map[string][]byte{pruneReleaseA: payload}}

			in := pruneInput(pruneReleaseA)
			if kind == "late release ref" {
				in.ReleaseImages = append(in.ReleaseImages, "registry.example/release:latest")
			}

			var out bytes.Buffer

			result, err := PruneBaseImages(t.Context(), pruner, verifier, &out, in)
			if kind == "late release ref" && len(verifier.refs) != 0 {
				t.Fatalf("malformed later input reached verifier: %v", verifier.refs)
			}

			if kind == "valid distinct hashes" {
				if err != nil || !slices.Equal(result.Referenced, []string{pruneIDKept}) || len(pruner.deleted) != 1 {
					t.Fatalf("err=%v result=%+v", err, result)
				}
			} else if err == nil || len(pruner.deleted) != 0 || pruner.listName != "" || out.Len() != 0 {
				t.Fatalf("err=%v deletes=%v listed=%s output=%s", err, pruner.deleted, pruner.listName, &out)
			}
		})
	}
}

func TestCleanupBoundary_LateEntryPreflight(t *testing.T) {
	t.Parallel()

	for _, malformed := range []bool{true, false} {
		first := BaseImageMetadata{Flavor: "rust", Tag: pruneRepo + ":" + pruneIDKept + "-rust", Ref: pruneRepo + "@" + promoteDigest, CandidateTag: pruneRepo + ":staging-" + pruneIDKept + "-rust", CandidateRef: pruneRepo + "@" + promoteDigest, BaseInputID: pruneIDKept}
		second := first
		second.Flavor, second.Tag, second.CandidateTag = "go", pruneRepo+":"+pruneIDKept+"-go", pruneRepo+":staging-"+pruneIDKept+"-go"

		registry := &fakeBaseImageRegistry{digests: map[string]string{first.Tag: promoteDigest, first.CandidateTag: promoteDigest, second.Tag: "sha256:" + strings.Repeat("b", 64)}}
		if malformed {
			second.Ref = "bad-ref"
		}

		cleaner := &fakeBaseImagePackageAPI{versions: []string{tagName(first.CandidateTag), tagName(second.CandidateTag)}}

		var out bytes.Buffer

		err := CleanupStagingBaseImages(t.Context(), registry, cleaner, &out, BaseImageCleanupStagingInput{ExpectedRepository: pruneRepo, Images: []BaseImageMetadata{first, second}})
		if !errors.Is(err, errs.ErrValidation) || len(cleaner.deleted) != 0 || cleaner.listName != "" || out.Len() != 0 {
			t.Fatalf("err=%v deleted=%v list=%s out=%s", err, cleaner.deleted, cleaner.listName, &out)
		}
	}
}
