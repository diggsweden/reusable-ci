// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import "path/filepath"

// CollectAssets dedupes a list of candidate file paths by basename,
// preserving declaration order. The bash uses an associative-array
// dedupe by basename — same approach so a pattern-glob match and a
// release-artefacts-dir match for the same basename don't double up.
func CollectAssets(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
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

// SignaturePath returns path + ".asc" — the canonical detached-signature
// path for a release asset.
func SignaturePath(path string) string { return path + ".asc" }
