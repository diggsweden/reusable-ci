// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// twoLedgerEntries returns two entries that differ in every field a request
// or predicate carries: image, digest, tags, SBOM, base image and base input.
func twoLedgerEntries(t *testing.T) (imageledger.Entry, imageledger.Entry) {
	t.Helper()

	first := ledgerSignEntry(t)

	secondDigest := "sha256:" + strings.Repeat("e", 64)
	second := imageledger.Entry{
		Role:         "runtime",
		Flavor:       "go",
		Ref:          "codeberg.org/itiquette/gommitlint@" + secondDigest,
		Digest:       secondDigest,
		SBOM:         "dist/image-sbom-go.cyclonedx.json",
		FinalTag:     "codeberg.org/itiquette/gommitlint:v1.2.3-go",
		MovingTag:    "codeberg.org/itiquette/gommitlint:go",
		CandidateTag: "codeberg.org/itiquette/gommitlint:staging-v1.2.3-go",
		BaseRef:      "codeberg.org/itiquette/gommitlint-base@sha256:" + strings.Repeat("f", 64),
		BaseInputID:  strings.Repeat("d", 64),
	}

	return first, second
}

func twoEntryInput(t *testing.T, first, second imageledger.Entry) appcontainer.SignLedgerImagesInput {
	t.Helper()

	return appcontainer.SignLedgerImagesInput{
		Entries:                 []imageledger.Entry{first, second},
		ReleaseTag:              "v1.2.3",
		PredicatePath:           writeBasePredicate(t),
		Method:                  domainrelease.SignMethodSigstore,
		Recursive:               true,
		OIDCIssuer:              "https://issuer.example.invalid",
		ExpectedImageRepository: "codeberg.org/itiquette/gommitlint",
		ExpectedBaseRepository:  "codeberg.org/itiquette/gommitlint-base",
	}
}

// TestSignLedgerImages_EveryRequestAndLineageBindsToItsOwnEntry signs two
// entries that share nothing and compares each cosign request whole, then
// each provenance document's image and base lineage whole. The two-entry
// ordering test compares only image references across entries with one
// digest, and the lineage test reads the base ref and one dependency URI, so
// a predicate built from the wrong entry, a dropped Recursive or issuer, or a
// dependency missing its digest or annotation all passed.
func TestSignLedgerImages_EveryRequestAndLineageBindsToItsOwnEntry(t *testing.T) {
	t.Chdir(t.TempDir())

	first, second := twoLedgerEntries(t)
	signer := &recordingImageSigner{}
	resolver := fakeLedgerResolver{first.CandidateTag: first.Digest, second.CandidateTag: second.Digest}

	if err := appcontainer.SignLedgerImages(t.Context(), signer, &fakeLedgerSyft{}, resolver, io.Discard, io.Discard, twoEntryInput(t, first, second)); err != nil {
		t.Fatal(err)
	}

	if len(signer.signs) != 2 || len(signer.attests) != 4 {
		t.Fatalf("signs = %d, attests = %d; want 2 and 4", len(signer.signs), len(signer.attests))
	}

	for index, entry := range []imageledger.Entry{first, second} {
		ref := entry.CandidateTag + "@" + entry.Digest
		identity := cosign.SignImageInput{ImageRef: ref, Recursive: true, Keyless: true, OIDCIssuer: "https://issuer.example.invalid"}

		if signer.signs[index] != identity {
			t.Errorf("entry %d sign request =\n%+v\nwant\n%+v", index, signer.signs[index], identity)
		}

		sbom := cosign.AttestImageInput{ImageRef: ref, PredicateType: "cyclonedx", PredicatePath: entry.SBOM, Recursive: true, Keyless: true, OIDCIssuer: "https://issuer.example.invalid"}
		if got := signer.attests[2*index].input; got != sbom {
			t.Errorf("entry %d SBOM attestation =\n%+v\nwant\n%+v", index, got, sbom)
		}

		provenance := signer.attests[2*index+1]
		wantProvenance := sbom
		wantProvenance.PredicateType = "slsaprovenance1"
		wantProvenance.PredicatePath = provenance.input.PredicatePath

		if provenance.input != wantProvenance || filepath.Base(provenance.input.PredicatePath) != "image-"+strconv.Itoa(index)+".predicate.json" {
			t.Errorf("entry %d provenance attestation = %+v", index, provenance.input)
		}

		assertEntryLineage(t, index, provenance.predicate, entry, ref)
	}
}

func assertEntryLineage(t *testing.T, index int, body []byte, entry imageledger.Entry, ref string) {
	t.Helper()

	var predicate map[string]any
	if err := json.Unmarshal(body, &predicate); err != nil {
		t.Fatalf("entry %d predicate: %v", index, err)
	}

	build := asMap(t, predicate["buildDefinition"], "buildDefinition")
	ext := asMap(t, build["externalParameters"], "externalParameters")

	wantImage := map[string]any{
		"ref": ref, "digest_ref": entry.Digest, "digest": map[string]any{"sha256": strings.TrimPrefix(entry.Digest, "sha256:")},
		"final_tag": entry.FinalTag, "moving_tag": entry.MovingTag, "candidate_tag": entry.CandidateTag,
		"role": entry.Role, "flavor": entry.Flavor, "sbom": entry.SBOM,
	}
	if image := asMap(t, ext["image"], "image"); !reflect.DeepEqual(image, wantImage) {
		t.Errorf("entry %d image =\n%#v\nwant\n%#v", index, image, wantImage)
	}

	wantBase := map[string]any{"ref": entry.BaseRef, "input_id": entry.BaseInputID}
	if base := asMap(t, ext["base"], "base"); !reflect.DeepEqual(base, wantBase) {
		t.Errorf("entry %d base =\n%#v\nwant\n%#v", index, base, wantBase)
	}

	wantDeps := []any{map[string]any{
		"uri":         "oci://" + entry.BaseRef,
		"digest":      map[string]any{"sha256": entry.BaseRef[strings.LastIndex(entry.BaseRef, ":")+1:]},
		"annotations": map[string]any{"base_input_id": entry.BaseInputID},
	}}
	if deps := build["resolvedDependencies"]; !reflect.DeepEqual(deps, wantDeps) {
		t.Errorf("entry %d resolvedDependencies =\n%#v\nwant\n%#v", index, deps, wantDeps)
	}
}

// failingImageSigner records every publication in order and fails the
// numbered call.
type failingImageSigner struct {
	recordingImageSigner

	calls  []string
	failAt int
}

var errPublicationRefused = errors.New("publication refused")

func (f *failingImageSigner) SignImage(ctx context.Context, in cosign.SignImageInput, out io.Writer) error {
	f.calls = append(f.calls, "sign")
	if len(f.calls) == f.failAt {
		return errPublicationRefused
	}

	return f.recordingImageSigner.SignImage(ctx, in, out)
}

func (f *failingImageSigner) AttestImage(ctx context.Context, in cosign.AttestImageInput, out io.Writer) error {
	f.calls = append(f.calls, "attest:"+in.PredicateType)
	if len(f.calls) == f.failAt {
		return errPublicationRefused
	}

	return f.recordingImageSigner.AttestImage(ctx, in, out)
}

// TestSignLedgerImages_APublicationFailureStopsLaterPublicationAndCleansUp
// fails each publication of a two-entry run in turn. Nothing is published
// after the failure, the error names the entry and keeps its cause, and the
// temporary predicates are gone. Both entries' generated SBOMs remain: they
// are local evidence prepared before the first signature, not remote effects.
// A retry of the same run then converges: every entry is signed and attested
// once more, each publication for its own digest reference and SBOM.
func TestSignLedgerImages_APublicationFailureStopsLaterPublicationAndCleansUp(t *testing.T) {
	all := []string{"sign", "attest:cyclonedx", "attest:slsaprovenance1", "sign", "attest:cyclonedx", "attest:slsaprovenance1"}

	for failAt := 1; failAt <= len(all); failAt++ {
		t.Run(all[failAt-1]+" of entry "+strconv.Itoa((failAt-1)/3), func(t *testing.T) {
			t.Chdir(t.TempDir())

			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)

			first, second := twoLedgerEntries(t)
			signer := &failingImageSigner{failAt: failAt}
			resolver := fakeLedgerResolver{first.CandidateTag: first.Digest, second.CandidateTag: second.Digest}

			err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, resolver, io.Discard, io.Discard, twoEntryInput(t, first, second))
			if !errors.Is(err, errPublicationRefused) || !strings.Contains(err.Error(), "entry "+strconv.Itoa((failAt-1)/3)) {
				t.Errorf("err = %v, want the refusal naming its entry", err)
			}

			if !slices.Equal(signer.calls, all[:failAt]) {
				t.Errorf("publications = %q, want %q", signer.calls, all[:failAt])
			}

			if leftovers, readErr := os.ReadDir(tmp); readErr != nil || len(leftovers) != 0 {
				t.Errorf("temporary predicates left behind: %v (err %v)", leftovers, readErr)
			}

			for _, entry := range []imageledger.Entry{first, second} {
				if _, statErr := os.Stat(entry.SBOM); statErr != nil {
					t.Errorf("prepared SBOM %s missing: %v", entry.SBOM, statErr)
				}
			}

			requireRetryConverges(t, first, second, resolver)
		})
	}
}

// requireRetryConverges retries the two-entry run with a healthy signer and
// requires two signatures and four attestations in entry order, each for its
// own digest reference, and each SBOM attestation for its own SBOM.
func requireRetryConverges(t *testing.T, first, second imageledger.Entry, resolver fakeLedgerResolver) {
	t.Helper()

	retry := &recordingImageSigner{}
	if err := appcontainer.SignLedgerImages(context.Background(), retry, &fakeLedgerSyft{}, resolver, io.Discard, io.Discard, twoEntryInput(t, first, second)); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if len(retry.signs) != 2 || len(retry.attests) != 4 {
		t.Fatalf("retry published %d signatures and %d attestations, want 2 and 4", len(retry.signs), len(retry.attests))
	}

	// The provenance predicate lives at a temporary path, so it is identified
	// by image and type; the SBOM by its own path too.
	published := make([]string, 0, len(retry.signs)+len(retry.attests))
	for _, sign := range retry.signs {
		published = append(published, "sign "+sign.ImageRef)
	}

	for _, attest := range retry.attests {
		entry := attest.input.ImageRef + " " + attest.input.PredicateType
		if attest.input.PredicateType == "cyclonedx" {
			entry += " " + attest.input.PredicatePath
		}

		published = append(published, entry)
	}

	firstRef, secondRef := first.CandidateTag+"@"+first.Digest, second.CandidateTag+"@"+second.Digest
	if want := []string{
		"sign " + firstRef, "sign " + secondRef,
		firstRef + " cyclonedx " + first.SBOM, firstRef + " slsaprovenance1",
		secondRef + " cyclonedx " + second.SBOM, secondRef + " slsaprovenance1",
	}; !slices.Equal(published, want) {
		t.Errorf("retry published %q, want %q", published, want)
	}
}
