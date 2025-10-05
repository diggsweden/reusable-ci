// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

// SelfRuntimeRepository is the repository allowed to publish the private
// self-runtime CLI channel.
const SelfRuntimeRepository = "diggsweden/reusable-ci"

// SelfRuntimeChannelTag is the one mutable CLI tag owned by the self-runtime
// workflow.
const SelfRuntimeChannelTag = "v3.0.0-pre"

// SelfRuntimeSourceRefAllowed reports whether ref is a branch trusted by both
// the rolling-channel producer and installer Sigstore identity policy.
func SelfRuntimeSourceRefAllowed(ref string) bool {
	switch ref {
	case "refs/heads/main", "refs/heads/feat/refactor-go":
		return true
	default:
		return false
	}
}
