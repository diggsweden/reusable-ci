// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import "regexp"

// commitSHARE matches a full-length git commit hash: 40 lowercase hex (sha1) or
// 64 lowercase hex (sha256). It is the single source of truth for commit-SHA
// shape validation — the release-provenance, sign-and-publish, and signer-image
// paths pin the same shape here rather than each re-deriving it (which had
// drifted into strict `40|64` and loose `40,64` spellings).
//
//nolint:gochecknoglobals // compiled regex, read-only.
var commitSHARE = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ValidCommitSHA reports whether s is a full-length git commit hash (exactly 40
// or 64 lowercase hex characters).
func ValidCommitSHA(s string) bool { return commitSHARE.MatchString(s) }
