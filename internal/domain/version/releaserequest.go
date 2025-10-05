// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import "strings"

// ReleaseRequestPrefix is the ref namespace a human pushes (signed) to
// request a release. The orchestrator promotes it to the final
// <version> tag once, at the bump commit — the request ref is itself
// never mutated or deleted, so it stays as the immutable, signed,
// allowlist-verified anchor of who authorised the release.
const ReleaseRequestPrefix = "release-request/"

// ReleaseRequestVersion maps a release-request ref to the final release
// tag it asks for. It accepts the short ref-name
// ("release-request/v3.5.7") or the fully-qualified form
// ("refs/tags/release-request/v3.5.7") and returns the final tag
// ("v3.5.7") with ok=true. Any ref that is not a release request — the
// final tag itself, a branch, the empty string — returns ("", false).
func ReleaseRequestVersion(ref string) (string, bool) {
	ref = strings.TrimPrefix(ref, "refs/tags/")

	tag, ok := strings.CutPrefix(ref, ReleaseRequestPrefix)
	if !ok || tag == "" {
		return "", false
	}

	return tag, true
}
