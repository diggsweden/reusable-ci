// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

// Merge concatenates the entries of several ledger documents into one,
// dropping exact duplicates while preserving first-seen order. It checks only
// that each document parses; per-entry release-scope and digest rules are the
// trust boundary's job (validate / verify-digests / promote), run on the
// merged result.
//
// Merge exists for the multi-container release: each container's build job
// writes its own single-entry ledger artifact, so promotion downloads them all
// and merges into one ledger that is validated, promoted, and rolled back as a
// single gated unit — matching the ledger's multi-entry design.
func Merge(docs [][]byte) ([]byte, error) {
	merged := make([]Entry, 0, len(docs))
	seen := make(map[Entry]bool) // Entry is a flat all-string struct: comparable.

	for _, doc := range docs {
		entries, err := Parse(doc)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if seen[entry] {
				continue
			}

			seen[entry] = true
			merged = append(merged, entry)
		}
	}

	return Marshal(merged)
}
