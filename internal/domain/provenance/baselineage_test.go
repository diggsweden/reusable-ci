// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

func TestBaseLineagePredicate_MatchesForgejoCIGoldenDigest(t *testing.T) {
	t.Parallel()

	body, err := provenance.BaseLineagePredicate(provenance.BaseLineageInput{
		Source:      "codeberg.org/itiquette/nanolinter",
		Commit:      strings.Repeat("c", 40),
		Workflow:    "container-bases.yml",
		Flavor:      "rust",
		BaseInputID: strings.Repeat("a", 64),
		Image:       "codeberg.org/itiquette/nanolinter-base@sha256:" + strings.Repeat("d", 64),
		BuildType:   "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])

	const want = "8e7ed2c28cf6bbf81a1cd72dbcdc47d906b280dc88f28d9c668a72efad5da16c"
	if got != want {
		t.Fatalf("base lineage predicate digest = %s, want %s\n%s", got, want, body)
	}
}

func TestBaseLineagePredicate_RejectsInvalidBaseInputID(t *testing.T) {
	t.Parallel()

	_, err := provenance.BaseLineagePredicate(provenance.BaseLineageInput{
		Source:      "codeberg.org/itiquette/nanolinter",
		Commit:      strings.Repeat("c", 40),
		Workflow:    "container-bases.yml",
		Flavor:      "rust",
		BaseInputID: "not-a-sha",
		Image:       "codeberg.org/itiquette/nanolinter-base@sha256:" + strings.Repeat("d", 64),
		BuildType:   "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}
