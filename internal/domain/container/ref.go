// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"regexp"
	"strings"
)

// digestRE matches a canonical OCI content digest: the lowercase `sha256:`
// prefix followed by 64 hex characters. This is the single source of truth
// for digest validation across the container, manifest, and image-ledger
// paths — a security invariant (a mutable tag must never pass where a pinned
// digest is required), so it lives in one place rather than being re-derived.
//
//nolint:gochecknoglobals // compiled regex, read-only.
var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidDigest reports whether s is a canonical `sha256:<64-hex>` digest.
func ValidDigest(s string) bool { return digestRE.MatchString(s) }

// StripTag removes a trailing `:tag` from an OCI reference, keeping the
// registry/path. A `:` that precedes the final `/` (a host:port, e.g.
// `localhost:5000/img`) is left alone, and registry-less refs (`alpine:3.21`)
// are handled. Idempotent for already-bare references.
func StripTag(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon]
	}

	return ref
}
