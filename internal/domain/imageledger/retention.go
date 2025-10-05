// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// UnreferencedBaseInputs returns the base-input IDs present in inventory that
// referenced does not mention: the promoted base images no supported release
// still depends on, and therefore the set a retention pass may delete.
//
// Base images are content-addressed by base-input ID, so a build never selects
// a stale one — it derives the ID from its own inputs and either finds that
// image or builds it. Retention is therefore not about correctness of future
// builds. An old base matters only for reproducing or re-verifying a release
// that was built on it, which is why the referenced set comes from releases
// rather than from anything about the base itself.
//
// The result is deduplicated and sorted, so a caller printing a dry run gets a
// stable list and two runs over the same registry produce the same output.
//
// This is a set difference and nothing more. It deliberately does not decide
// whether the difference is safe to act on: an empty referenced set means
// "delete everything", which is a legitimate answer here and a bug almost
// anywhere else. The caller owns that judgement — see PruneBaseImages, which
// refuses it.
func UnreferencedBaseInputs(inventory, referenced []string) ([]string, error) {
	keep := make(map[string]struct{}, len(referenced))

	for _, id := range referenced {
		normalized, err := normalizeBaseInputID(id, "referenced")
		if err != nil {
			return nil, err
		}

		keep[normalized] = struct{}{}
	}

	var prunable []string

	for _, id := range inventory {
		normalized, err := normalizeBaseInputID(id, "inventory")
		if err != nil {
			return nil, err
		}

		if _, referenced := keep[normalized]; referenced {
			continue
		}

		if !slices.Contains(prunable, normalized) {
			prunable = append(prunable, normalized)
		}
	}

	slices.Sort(prunable)

	return prunable, nil
}

// normalizeBaseInputID trims a base-input ID and rejects anything that is not a
// bare sha256 hex digest.
//
// Rejecting rather than skipping is the safe direction. An ID this function
// cannot parse is one the caller also cannot match against the referenced set,
// so treating it as unrecognised would silently classify a live base as
// prunable. Failing the whole pass instead turns an unexpected tag shape into a
// question rather than a deletion.
func normalizeBaseInputID(id, source string) (string, error) {
	normalized := strings.TrimSpace(id)

	if normalized == "" {
		return "", fmt.Errorf("imageledger: empty %s base-input id: %w", source, errs.ErrValidation)
	}

	if !container.ValidSHA256Hex(normalized) {
		return "", fmt.Errorf("imageledger: %s base-input id must be a sha256 hex digest: %q: %w",
			source, normalized, errs.ErrValidation)
	}

	return normalized, nil
}
