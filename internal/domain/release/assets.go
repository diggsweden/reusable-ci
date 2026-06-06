// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import "path/filepath"

// CollectAssets dedupes a list of candidate file paths by basename,
// preserving declaration order. The bash uses an associative-array
// dedupe by basename — same approach so a pattern-glob match and a
// release-artefacts-dir match for the same basename don't double up.
func CollectAssets(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))

	out := make([]string, 0, len(candidates))
	for _, p := range candidates { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if p == "" {
			continue
		}

		base := filepath.Base(p)
		if _, dup := seen[base]; dup {
			continue
		}

		seen[base] = struct{}{}

		out = append(out, p)
	}

	return out
}

// SignatureSidecars returns every possible signature sidecar path
// next to a release asset, across all supported signing methods.
// The release-create flow scans the result and attaches whichever
// files exist on disk — the producer chose one method per repo
// (`sign.method` in artifacts.yml), so in practice exactly one of
// the returned paths will exist.
//
// Order is stable; callers should not assume a specific layout.
func SignatureSidecars(path string) []string {
	return []string{
		path + ".asc",    // gpg detached signature
		path + ".bundle", // cosign v3 bundle (sigstore + kms)
	}
}
