// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
)

func TestMerge_CombinesAndDeduplicates(t *testing.T) {
	t.Parallel()

	a := []byte(`[{"ref":"ghcr.io/o/a@` + goodDigest + `","digest":"` + goodDigest + `","final_tag":"ghcr.io/o/a:v1.2.3"}]`)
	b := []byte(`[{"ref":"ghcr.io/o/b@` + goodDigest + `","digest":"` + goodDigest + `","final_tag":"ghcr.io/o/b:v1.2.3"}]`)

	out, err := imageledger.Merge([][]byte{a, b, a}) // a repeated → deduped
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	entries, err := imageledger.Parse(out)
	if err != nil {
		t.Fatalf("parse merged: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("want 2 deduped entries, got %d: %s", len(entries), out)
	}

	if entries[0].FinalTag != "ghcr.io/o/a:v1.2.3" || entries[1].FinalTag != "ghcr.io/o/b:v1.2.3" {
		t.Errorf("first-seen order not preserved: %+v", entries)
	}
}

func TestMerge_EmptyAndNil(t *testing.T) {
	t.Parallel()

	out, err := imageledger.Merge(nil)
	if err != nil {
		t.Fatalf("merge nil: %v", err)
	}

	if string(out) != "[]\n" {
		t.Errorf("empty merge = %q, want %q", out, "[]\n")
	}
}

func TestMerge_RejectsMalformed(t *testing.T) {
	t.Parallel()

	if _, err := imageledger.Merge([][]byte{[]byte(`{not an array}`)}); err == nil {
		t.Error("expected error on malformed ledger document")
	}
}

func TestDeriveTags_MatchesValidatorScoping(t *testing.T) {
	t.Parallel()

	final, candidate := imageledger.DeriveTags("ghcr.io/org/app", "v1.2.3")
	if final != "ghcr.io/org/app:v1.2.3" || candidate != "ghcr.io/org/app:staging-v1.2.3" {
		t.Fatalf("DeriveTags = %q, %q", final, candidate)
	}

	// The derived pair must satisfy Entry.Validate at the same release tag.
	e := imageledger.Entry{
		Ref:          "ghcr.io/org/app@" + goodDigest,
		Digest:       goodDigest,
		FinalTag:     final,
		CandidateTag: candidate,
	}
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("derived tags rejected by Validate: %v", err)
	}
}
