// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DigestPattern is the canonical OCI content digest shape: the lowercase
// `sha256:` prefix followed by 64 hex characters. This is the single source
// of truth for digest validation across the container, manifest, and
// image-ledger paths — a security invariant (a mutable tag must never pass
// where a pinned digest is required), so it lives in one place rather than
// being re-derived. Exported so schema renderers embed the enforced rule
// instead of re-spelling it.
const DigestPattern = `^sha256:[0-9a-f]{64}$`

//nolint:gochecknoglobals // compiled regex, read-only.
var digestRE = regexp.MustCompile(DigestPattern)

// ValidDigest reports whether s is a canonical `sha256:<64-hex>` digest.
func ValidDigest(s string) bool { return digestRE.MatchString(s) }

// SHA256HexPattern is a bare sha256 as 64 lowercase hex characters, without
// the `sha256:` algorithm prefix — the shape a content ID (base-input-id), an
// SBOM content pin, or a digest's hex tail takes. Kept beside DigestPattern
// so the two hash-shape invariants are single-sourced together rather than
// re-derived in every package that records or re-validates ledger entries.
// Exported for the same schema-rendering reason as DigestPattern.
const SHA256HexPattern = `^[0-9a-f]{64}$`

//nolint:gochecknoglobals // compiled regex, read-only.
var sha256HexRE = regexp.MustCompile(SHA256HexPattern)

// ValidSHA256Hex reports whether s is 64 lowercase hex characters — a bare
// sha256 with no `sha256:` prefix.
func ValidSHA256Hex(s string) bool { return sha256HexRE.MatchString(s) }

// digestPinnedRefRE matches a fully digest-pinned image reference with NO tag
// permitted: registry/path@sha256:<64-hex>. The base-image and signer-image
// trust boundaries require the exact digest form, so they share this pattern
// rather than each re-compiling it. (The image ledger deliberately keeps its
// own tag-permitting variant, imageledger.imageRefRE, which is a distinct rule.)
//
//nolint:gochecknoglobals // compiled regex, read-only.
var digestPinnedRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+@sha256:[0-9a-f]{64}$`)

// ValidDigestPinnedRef reports whether s is a tag-free, digest-pinned image
// reference (registry/path@sha256:<64-hex>).
func ValidDigestPinnedRef(s string) bool { return digestPinnedRefRE.MatchString(s) }

// OCITagComponent is the unanchored regex fragment for a single OCI tag
// component: the value after `:` in a reference, and equally a promotion stage
// name (which is used verbatim as that tag). It is the single source of the
// tag charset — an alphanumeric/underscore lead followed by up to 127 more of
// `[A-Za-z0-9._-]` — so the ledger's composed ref patterns and the stage-name
// validator embed THIS constant rather than re-spelling the class. Kept
// unanchored so callers can splice it into a larger pattern; the anchored
// standalone form is ociTagComponentRE / ValidOCITagComponent.
const OCITagComponent = `[A-Za-z0-9_][A-Za-z0-9._-]{0,127}`

//nolint:gochecknoglobals // compiled regex, read-only.
var ociTagComponentRE = regexp.MustCompile(`^` + OCITagComponent + `$`)

// ValidOCITagComponent reports whether s is a single valid OCI tag component
// (also the rule for a promotion stage name).
func ValidOCITagComponent(s string) bool { return ociTagComponentRE.MatchString(s) }

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

// StripDigest removes a trailing @<algorithm>:<digest> from an OCI reference.
// It is intentionally syntax-light: callers that need full validation should do
// that separately, while this helper only computes the repository/tag part for
// comparison or destination derivation.
func StripDigest(ref string) string {
	if at := strings.Index(ref, "@"); at >= 0 {
		return ref[:at]
	}

	return ref
}

// StripTagOrDigest returns the repository path with either a mutable tag or a
// digest suffix removed. A ref that carries both (`repo:tag@sha256:...`) loses
// both, yielding `repo`.
func StripTagOrDigest(ref string) string {
	return StripTag(StripDigest(ref))
}

// ImageNameWithoutTag returns ref's repository/name with a trailing tag removed,
// preserving any registry host:port prefix.
func ImageNameWithoutTag(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("image repository name is empty: %w", errs.ErrUsage)
	}

	name := StripTag(ref)

	last := name[strings.LastIndex(name, "/")+1:]
	if last == "" {
		return "", fmt.Errorf("image repository name is empty after removing tag: %s: %w", ref, errs.ErrUsage)
	}

	return name, nil
}

// CanonicalImageRef removes docker:// and canonicalizes digest refs by dropping
// any tag before @sha256:..., matching Buildah/Skopeo's digest-ref expectation.
func CanonicalImageRef(ref string) (string, error) {
	if ref == "" || strings.ContainsAny(ref, "\n\r") {
		return "", fmt.Errorf("unsafe or empty image ref: %w", errs.ErrUsage)
	}

	if strings.HasPrefix(ref, "docker://") {
		ref = strings.TrimPrefix(ref, "docker://")
	} else if strings.Contains(ref, "://") {
		return "", fmt.Errorf("image ref must not include a transport other than docker://: %s: %w", ref, errs.ErrUsage)
	}

	if strings.Count(ref, "@") > 1 {
		return "", fmt.Errorf("image ref contains multiple digest separators: %s: %w", ref, errs.ErrUsage)
	}

	name, digest, hasDigest := strings.Cut(ref, "@")
	if !hasDigest {
		return ref, nil
	}

	if name == "" || digest == "" {
		return "", fmt.Errorf("image digest ref must include both name and digest: %s: %w", ref, errs.ErrUsage)
	}

	name, err := ImageNameWithoutTag(name)
	if err != nil {
		return "", err
	}

	return name + "@" + digest, nil
}

// ImageNameForRef returns the repository/name for a tag or digest ref.
func ImageNameForRef(ref string) (string, error) {
	canonical, err := CanonicalImageRef(ref)
	if err != nil {
		return "", err
	}

	name, _, _ := strings.Cut(canonical, "@")

	return ImageNameWithoutTag(name)
}
