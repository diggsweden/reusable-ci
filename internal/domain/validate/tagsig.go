// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"regexp"
	"strings"
)

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

// goodSignaturePattern captures the signer identity from a `git tag -v`
// output line of the form: `gpg: Good signature from "Name <email>"`.
var goodSignaturePattern = regexp.MustCompile(`Good signature from "([^"]+)"`)

// ParseGoodSignerFromVerify extracts the signer identity from
// `git tag -v` combined output. Returns "" when no Good signature line
// is present (e.g. verification ok but the line is missing, or the
// signature was not verifiable).
func ParseGoodSignerFromVerify(verifyOutput string) string {
	m := goodSignaturePattern.FindStringSubmatch(verifyOutput)
	if m == nil {
		return ""
	}
	return m[1]
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
