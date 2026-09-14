// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
// canonical PGP / SSH signature header lines. Git appends a signature to the
// tag object as its own lines, so only an armor header that starts a line
// counts; the same words inside the tag message are prose.
func DetectTagSignatures(body string) SignaturePresence {
	return SignaturePresence{
		HasGPG: hasArmorLine(body, "-----BEGIN PGP SIGNATURE-----"),
		HasSSH: hasArmorLine(body, "-----BEGIN SSH SIGNATURE-----"),
	}
}

func hasArmorLine(body, header string) bool {
	return strings.HasPrefix(body, header) || strings.Contains(body, "\n"+header)
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
