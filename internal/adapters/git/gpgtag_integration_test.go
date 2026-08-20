//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// VerifyTagSignature answers "is this tag signed, and by whom" -- the
// fingerprint it returns is what the allowlist in app/validate/tags.go
// compares against, so both halves of the answer decide whether a
// release publishes.
//
// Every existing test here used an unsigned tag or no keyring at all, so
// the entire verifying path was untested: ok=true had never been produced
// from real signed data, and no test signed with one key and offered
// another. The tests below do both.
//
// The tag is signed in-process through go-git rather than by shelling out
// to gpg: no GNUPGHOME, no agent, no gpg binary.
//
// No t.Parallel(): isolatedgit.NewRepo scrubs the environment via
// t.Setenv.

// signedTagRepo creates an isolated repo carrying an annotated tag signed
// by a freshly minted entity, and returns the entity.
func signedTagRepo(t *testing.T, tag string) (*isolatedgit.Repo, *gocrypto.Entity) {
	t.Helper()

	repo := isolatedgit.NewRepo(t)

	entity, err := gocrypto.NewEntity("Release Bot", "ci", "bot@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}

	gr, err := gogit.PlainOpen(repo.Dir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}

	head, err := gr.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	_, err = gr.CreateTag(tag, head.Hash(), &gogit.CreateTagOptions{
		Tagger:  &object.Signature{Name: "Release Bot", Email: "bot@example.invalid", When: time.Unix(1700000000, 0).UTC()},
		Message: "release " + tag + "\n",
		SignKey: entity,
	})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	return repo, entity
}

func publicArmor(t *testing.T, entity *gocrypto.Entity) []byte {
	t.Helper()

	var buf bytes.Buffer

	w, err := armor.Encode(&buf, gocrypto.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := entity.Serialize(w); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// TestVerifyTagSignature_ReportsTheSignerAndFingerprint covers the answer
// the allowlist consumes. The fingerprint has to be the 40-char uppercase
// hex form, because that is the form PrimaryFingerprints derives from
// allowed_gpg_keys.asc and the two are compared as strings.
func TestVerifyTagSignature_ReportsTheSignerAndFingerprint(t *testing.T) {
	repo, entity := signedTagRepo(t, "v1.0.0")

	signer, fingerprint, ok, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSignature(context.Background(), "v1.0.0", publicArmor(t, entity))
	if err != nil {
		t.Fatal(err)
	}

	if !ok {
		t.Fatal("a tag signed by the key in the keyring did not verify")
	}

	want := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))
	if fingerprint != want {
		t.Errorf("fingerprint = %q, want %q -- the allowlist compares this as a string", fingerprint, want)
	}

	if signer != "Release Bot <bot@example.invalid>" {
		t.Errorf("signer = %q, want %q", signer, "Release Bot <bot@example.invalid>")
	}
}

// TestVerifyTagSignature_RejectsASignatureFromAnotherKey is the identity
// half. Without it, "ok=true" is satisfied by anything that notices a
// signature is present and well-formed.
//
// The contract is deliberate about the shape of the refusal: a failed
// verify is not a function-level error, it is ok=false with err=nil. A
// caller that only checked err would publish an unverified tag, so the
// test asserts both.
func TestVerifyTagSignature_RejectsASignatureFromAnotherKey(t *testing.T) {
	repo, _ := signedTagRepo(t, "v1.0.0")

	other, err := gocrypto.NewEntity("Someone Else", "ci", "else@example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}

	signer, fingerprint, ok, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSignature(context.Background(), "v1.0.0", publicArmor(t, other))
	if err != nil {
		t.Fatalf("a signature from an unrelated key must be ok=false with err=nil, got err = %v", err)
	}

	if ok {
		t.Fatal("a tag signed by a key outside the keyring verified")
	}

	if signer != "" || fingerprint != "" {
		t.Errorf("an unverified tag reported a signer: (%q, %q), want both empty", signer, fingerprint)
	}
}

// TestVerifyTagSignature_RejectsATamperedTag covers the other direction:
// the signature is by the right key, but the object it covers changed.
// The tag object is rewritten with a different message and the same
// signature block, which is what an edited tag looks like.
func TestVerifyTagSignature_RejectsATamperedTag(t *testing.T) {
	repo, entity := signedTagRepo(t, "v1.0.0")

	gr, err := gogit.PlainOpen(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := gr.Tag("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	original, err := gr.TagObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}

	tampered := *original
	tampered.Message = "release v9.9.9 -- rewritten\n"

	encoded := gr.Storer.NewEncodedObject()
	if err := tampered.Encode(encoded); err != nil {
		t.Fatal(err)
	}

	hash, err := gr.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if err := gr.Storer.SetReference(plumbing.NewHashReference(ref.Name(), hash)); err != nil {
		t.Fatal(err)
	}

	_, _, ok, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSignature(context.Background(), "v1.0.0", publicArmor(t, entity))
	if err != nil {
		t.Fatalf("a tampered tag must be ok=false with err=nil, got err = %v", err)
	}

	if ok {
		t.Fatal("a tag whose message was rewritten after signing still verified")
	}
}

// TestVerifyTagSignature_LightweightTagIsRefused keeps "not annotated"
// separate from "not signed". A lightweight tag carries no signature and
// no tagger, so treating it as merely unsigned would let a release
// through on a tag nobody could have signed.
func TestVerifyTagSignature_LightweightTagIsRefused(t *testing.T) {
	repo, entity := signedTagRepo(t, "v1.0.0")
	repo.Git("tag", "lightweight")

	_, _, ok, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSignature(context.Background(), "lightweight", publicArmor(t, entity))
	if ok {
		t.Fatal("a lightweight tag verified")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation naming the tag as not annotated", err)
	}
}
