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
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// errAttestationUnverified stands in for cosign refusing a signature.
var errAttestationUnverified = errors.New("signature does not verify")

const (
	pruneRepo = "registry.example/owner/project-base"

	pruneIDKept  = "1111111111111111111111111111111111111111111111111111111111111111"
	pruneIDStale = "2222222222222222222222222222222222222222222222222222222222222222"
	pruneIDOther = "3333333333333333333333333333333333333333333333333333333333333333"

	// Release images must be digest-pinned: the keep-set is only as
	// trustworthy as the refs it is derived from.
	pruneReleaseA = "registry.example/owner/project@sha256:" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pruneReleaseB = "registry.example/owner/project@sha256:" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// prunePayload builds a cosign verify-attestation body naming baseInputID.
// asArray covers cosign's other output shape: it emits a bare envelope for one
// attestation and a JSON array for several.
func prunePayload(t *testing.T, baseInputID string, asArray bool) []byte {
	t.Helper()

	statement := map[string]any{
		"predicateType": provenance.PredicateTypeV1,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"externalParameters": map[string]any{
					"base": map[string]any{
						"ref":      pruneRepo + "@sha256:" + baseInputID,
						"input_id": baseInputID,
					},
				},
			},
		},
	}

	body, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal statement: %v", err)
	}

	envelope := map[string]string{"payload": base64.StdEncoding.EncodeToString(body)}

	var out bytes.Buffer

	var encodeErr error

	if asArray {
		encodeErr = json.NewEncoder(&out).Encode([]any{envelope})
	} else {
		encodeErr = json.NewEncoder(&out).Encode(envelope)
	}

	if encodeErr != nil {
		t.Fatalf("marshal envelope: %v", encodeErr)
	}

	return out.Bytes()
}

// fakePruneVerifier answers per image ref, so a test can give one release a
// good attestation and another a broken one.
type fakePruneVerifier struct {
	payloads map[string][]byte
	err      error
}

func (f *fakePruneVerifier) VerifyImage(context.Context, cosign.VerifyImageInput, io.Writer) error {
	return nil
}

func (f *fakePruneVerifier) VerifyAttestation(context.Context, cosign.VerifyAttestationInput, io.Writer) error {
	return nil
}

func (f *fakePruneVerifier) VerifyAttestationOutput(_ context.Context, in cosign.VerifyAttestationInput, out, _ io.Writer) error {
	if f.err != nil {
		return f.err
	}

	_, err := out.Write(f.payloads[in.ImageRef])

	return err
}

func pruneInput(images ...string) BaseImagePruneInput {
	return BaseImagePruneInput{
		ExpectedRepository: pruneRepo,
		ReleaseImages:      images,
		MaxDelete:          10,
	}
}

// TestPruneBaseImagesDeletesOnlyUnreferenced is the core behaviour: the base a
// supported release was built on survives, the one nothing references does not.
func TestPruneBaseImagesDeletesOnlyUnreferenced(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: prunePayload(t, pruneIDKept, false),
	}}

	result, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(pruneReleaseA))
	if err != nil {
		t.Fatalf("PruneBaseImages() error = %v", err)
	}

	if len(pruner.deleted) != 1 || pruner.deleted[0] != pruneRepo+":"+pruneIDStale {
		t.Fatalf("deleted = %v, want only the unreferenced base tag", pruner.deleted)
	}

	if len(result.Deleted) != 1 || result.Deleted[0] != pruneIDStale {
		t.Errorf("result.Deleted = %v, want [%s]", result.Deleted, pruneIDStale)
	}

	if len(result.Referenced) != 1 || result.Referenced[0] != pruneIDKept {
		t.Errorf("result.Referenced = %v, want [%s]", result.Referenced, pruneIDKept)
	}
}

// TestPruneBaseImagesReadsBothCosignOutputShapes: a multi-attestation image
// yields an array, and reading only the object shape would make it look
// unreferenced — deleting a base a supported release depends on.
func TestPruneBaseImagesReadsBothCosignOutputShapes(t *testing.T) {
	t.Parallel()

	for _, asArray := range []bool{false, true} {
		t.Run(map[bool]string{false: "bare envelope", true: "envelope array"}[asArray], func(t *testing.T) {
			t.Parallel()

			pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
			verifier := &fakePruneVerifier{payloads: map[string][]byte{
				pruneReleaseA: prunePayload(t, pruneIDKept, asArray),
			}}

			result, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(pruneReleaseA))
			if err != nil {
				t.Fatalf("PruneBaseImages() error = %v", err)
			}

			if len(result.Referenced) != 1 || result.Referenced[0] != pruneIDKept {
				t.Fatalf("referenced = %v, want the base named by the attestation", result.Referenced)
			}
		})
	}
}

// TestPruneBaseImagesDryRunDeletesNothing keeps the default safe: a dry run
// must report the same list it would delete, and touch nothing.
func TestPruneBaseImagesDryRunDeletesNothing(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: prunePayload(t, pruneIDKept, false),
	}}

	in := pruneInput(pruneReleaseA)
	in.DryRun = true

	var out bytes.Buffer

	result, err := PruneBaseImages(context.Background(), pruner, verifier, &out, in)
	if err != nil {
		t.Fatalf("PruneBaseImages() error = %v", err)
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("dry run deleted %v, want nothing", pruner.deleted)
	}

	if len(result.Prunable) != 1 || result.Prunable[0] != pruneIDStale {
		t.Errorf("Prunable = %v, want the stale id reported", result.Prunable)
	}

	if !strings.Contains(out.String(), pruneIDStale) {
		t.Errorf("dry run output does not name what it would delete: %q", out.String())
	}
}

// TestPruneBaseImagesRefusesEmptyKeepSet is the guard that matters most. No
// release naming a base means the keep-set is wrong, not that every base is
// garbage — deleting the inventory on that basis is unrecoverable.
func TestPruneBaseImagesRefusesEmptyKeepSet(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{}}

	_, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput())
	if err == nil {
		t.Fatal("PruneBaseImages() = nil error, want a refusal to prune the whole inventory")
	}

	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("error = %v, want errs.ErrUsage", err)
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("deleted %v before refusing, want nothing", pruner.deleted)
	}
}

// TestPruneBaseImagesRefusesUnverifiableAttestation: an attestation that does
// not verify must abort, never be read as "this release references no base".
func TestPruneBaseImagesRefusesUnverifiableAttestation(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
	verifier := &fakePruneVerifier{err: errAttestationUnverified}

	_, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(pruneReleaseA))
	if err == nil {
		t.Fatal("PruneBaseImages() = nil error, want the unverifiable attestation to abort the pass")
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("deleted %v despite an unverifiable attestation, want nothing", pruner.deleted)
	}
}

// TestPruneBaseImagesRefusesAttestationWithoutBase: a release image whose
// predicate names no base is equally unusable as evidence.
func TestPruneBaseImagesRefusesAttestationWithoutBase(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDStale}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: []byte(`{"payload":"e30="}`), // {} — verified, but no lineage
	}}

	_, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(pruneReleaseA))
	if err == nil {
		t.Fatal("PruneBaseImages() = nil error, want an attestation without base lineage to abort")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("error = %v, want errs.ErrValidation", err)
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("deleted %v, want nothing", pruner.deleted)
	}
}

// TestPruneBaseImagesHonoursMaxDelete bounds the blast radius of one pass.
func TestPruneBaseImagesHonoursMaxDelete(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale, pruneIDOther}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: prunePayload(t, pruneIDKept, false),
	}}

	in := pruneInput(pruneReleaseA)
	in.MaxDelete = 1

	_, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, in)
	if err == nil {
		t.Fatal("PruneBaseImages() = nil error, want the max-delete bound to refuse")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("error = %v, want errs.ErrValidation", err)
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("deleted %v before hitting the bound, want nothing", pruner.deleted)
	}
}

// TestPruneBaseImagesIgnoresStagingAndForeignTags: staging tags belong to
// CleanupStagingBaseImages, and anything not shaped like a base-input id is
// not something this pass can identify, so it leaves both alone.
func TestPruneBaseImagesIgnoresStagingAndForeignTags(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{
		pruneIDKept,
		"staging-" + pruneIDStale + "-amd64",
		"v1.2.3",
		"latest",
	}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: prunePayload(t, pruneIDKept, false),
	}}

	result, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(pruneReleaseA))
	if err != nil {
		t.Fatalf("PruneBaseImages() error = %v", err)
	}

	if len(pruner.deleted) != 0 {
		t.Errorf("deleted %v, want nothing — the only base-shaped tag is referenced", pruner.deleted)
	}

	if len(result.Inventory) != 1 || result.Inventory[0] != pruneIDKept {
		t.Errorf("Inventory = %v, want only the base-shaped tag", result.Inventory)
	}
}

// TestPruneBaseImagesKeepsEveryReferencedBase: supported releases need not
// share a base. Each one's attestation contributes to the keep-set, and only
// what no release names is pruned.
func TestPruneBaseImagesKeepsEveryReferencedBase(t *testing.T) {
	t.Parallel()

	pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDOther, pruneIDStale}}
	verifier := &fakePruneVerifier{payloads: map[string][]byte{
		pruneReleaseA: prunePayload(t, pruneIDKept, false),
		pruneReleaseB: prunePayload(t, pruneIDOther, true),
	}}

	result, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard,
		pruneInput(pruneReleaseA, pruneReleaseB))
	if err != nil {
		t.Fatalf("PruneBaseImages() error = %v", err)
	}

	if len(result.Referenced) != 2 {
		t.Fatalf("Referenced = %v, want both bases kept alive", result.Referenced)
	}

	if len(pruner.deleted) != 1 || pruner.deleted[0] != pruneRepo+":"+pruneIDStale {
		t.Errorf("deleted = %v, want only the base no release names", pruner.deleted)
	}
}

// TestPruneBaseImagesRefusesUnpinnedReleaseImage is the guard against deriving
// the keep-set from a mutable tag. A tag serves whatever it points at now; if a
// release was retagged, cosign would verify a different image whose attestation
// names a different base, and the pass would keep the wrong one and prune the
// right one. A wrong-but-verifiable attestation is indistinguishable from a
// correct one, so this has to be refused at the input.
func TestPruneBaseImagesRefusesUnpinnedReleaseImage(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"registry.example/owner/project:v1.2.3",                       // a plain tag
		"registry.example/owner/project:v1.2.3@sha256:" + pruneIDKept, // tag AND digest
		"registry.example/owner/project",                              // neither
		"@sha256:" + pruneIDKept,                                      // digest, no repository
	} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()

			pruner := &fakeBaseImagePackageAPI{versions: []string{pruneIDKept, pruneIDStale}}
			verifier := &fakePruneVerifier{payloads: map[string][]byte{}}

			_, err := PruneBaseImages(context.Background(), pruner, verifier, io.Discard, pruneInput(ref))
			if err == nil {
				t.Fatalf("PruneBaseImages() with %q = nil error, want the unpinned ref refused", ref)
			}

			if !errors.Is(err, errs.ErrUsage) {
				t.Errorf("error = %v, want errs.ErrUsage", err)
			}

			// Refused before any registry call, so nothing was listed or deleted.
			if pruner.listOwner != "" || len(pruner.deleted) != 0 {
				t.Errorf("touched the registry before refusing: listed=%q deleted=%v", pruner.listOwner, pruner.deleted)
			}
		})
	}
}
