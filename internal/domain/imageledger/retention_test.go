// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// Distinct 64-hex base-input IDs. Spelled as repeats so a failure message
// names which one moved rather than showing two near-identical digests.
const (
	idA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	idB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	idC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestUnreferencedBaseInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		inventory  []string
		referenced []string
		want       []string
	}{
		{
			name:      "everything referenced leaves nothing to prune",
			inventory: []string{idA, idB}, referenced: []string{idA, idB},
			want: nil,
		},
		{
			name:      "unreferenced ids are prunable",
			inventory: []string{idA, idB, idC}, referenced: []string{idB},
			want: []string{idA, idC},
		},
		{
			// The honest answer to "no release references anything". Acting on
			// it is the caller's decision, not this function's.
			name:      "empty referenced set prunes the whole inventory",
			inventory: []string{idA, idB}, referenced: nil,
			want: []string{idA, idB},
		},
		{
			// A release built on a base that has already been deleted, or on
			// one held in another registry. Nothing to do, not an error.
			name:      "referenced ids absent from inventory are ignored",
			inventory: []string{idA}, referenced: []string{idB, idC},
			want: []string{idA},
		},
		{
			name:      "empty inventory prunes nothing",
			inventory: nil, referenced: []string{idA},
			want: nil,
		},
		{
			name:      "duplicates collapse",
			inventory: []string{idB, idA, idB, idA}, referenced: nil,
			want: []string{idA, idB},
		},
		{
			name:      "surrounding whitespace does not hide a reference",
			inventory: []string{" " + idA + "\n", idB}, referenced: []string{"\t" + idA},
			want: []string{idB},
		},
		{
			// Sorted output, so a dry run prints the same list twice running.
			name:      "result is sorted regardless of inventory order",
			inventory: []string{idC, idA, idB}, referenced: nil,
			want: []string{idA, idB, idC},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := imageledger.UnreferencedBaseInputs(tc.inventory, tc.referenced)
			if err != nil {
				t.Fatalf("UnreferencedBaseInputs() error = %v, want nil", err)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestUnreferencedBaseInputsRejectsUnparseableIDs is the safety property: an ID
// this function cannot parse is one it also cannot match against the referenced
// set, so silently treating it as unrecognised would classify a live base as
// prunable. Every malformed input must fail the pass instead.
func TestUnreferencedBaseInputsRejectsUnparseableIDs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		inventory  []string
		referenced []string
	}{
		{"empty inventory id", []string{idA, ""}, nil},
		{"empty referenced id", []string{idA}, []string{""}},
		{"whitespace-only inventory id", []string{"   "}, nil},
		{"digest-prefixed inventory id", []string{"sha256:" + idA}, nil},
		{"short inventory id", []string{idA[:63]}, nil},
		{"long inventory id", []string{idA + "a"}, nil},
		{"uppercase inventory id", []string{strings.ToUpper(idA)}, nil},
		{"non-hex inventory id", []string{strings.Repeat("z", 64)}, nil},
		{"tag-shaped inventory id", []string{"staging-" + idA}, nil},
		{"malformed referenced id", []string{idA}, []string{"not-a-digest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := imageledger.UnreferencedBaseInputs(tc.inventory, tc.referenced)
			if err == nil {
				t.Fatalf("UnreferencedBaseInputs() = %v, want a validation error", got)
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("error = %v, want errs.ErrValidation so the exit code classifies", err)
			}

			if got != nil {
				t.Errorf("result = %v, want nil alongside the error", got)
			}
		})
	}
}
