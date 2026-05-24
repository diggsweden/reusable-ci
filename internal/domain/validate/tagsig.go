// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import "strings"

// SignaturePresence reports which signature blocks are embedded in an
// annotated tag's body. Tags can carry GPG, SSH, or both.
type SignaturePresence struct {
	HasGPG bool
	HasSSH bool
}

// Any reports whether the tag has any kind of signature.
func (s SignaturePresence) Any() bool { return s.HasGPG || s.HasSSH }

// DetectTagSignatures scans a `git cat-file tag <tag>` body for the
// canonical PGP / SSH signature header lines.
func DetectTagSignatures(body string) SignaturePresence {
	return SignaturePresence{
		HasGPG: strings.Contains(body, "BEGIN PGP SIGNATURE"),
		HasSSH: strings.Contains(body, "BEGIN SSH SIGNATURE"),
	}
}

// FilterOutTag returns tags with the named one removed. Used by
// tag-uniqueness to distinguish "self-referential" matches from genuine
// collisions.
func FilterOutTag(tags []string, drop string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t == drop {
			continue
		}

		out = append(out, t)
	}

	return out
}
