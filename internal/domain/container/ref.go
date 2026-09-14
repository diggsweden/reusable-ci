// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"

	ociname "github.com/google/go-containerregistry/pkg/name"
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

// ValidDigestPinnedRef reports whether s is a tag-free, digest-pinned image
// reference (registry/path@sha256:<64-hex>).
func ValidDigestPinnedRef(ref string) bool {
	ref = explicitRegistryName(ref)
	parsed, err := ociname.NewDigest(ref, ociname.StrictValidation)

	// NewDigest accepts tag@digest and deliberately drops the tag from Name.
	// Requiring exact reproduction preserves this function's tag-free contract.
	return err == nil && parsed.Name() == ref && validRepositorySegments(parsed.RepositoryStr())
}

// ValidDigestReference reports whether ref is a fully qualified digest
// reference with an optional tag before the digest. Promotion journals retain
// the source tag in this form while pinning the manifest with the digest.
func ValidDigestReference(ref string) bool {
	ref = explicitRegistryName(ref)

	parsed, err := ociname.NewDigest(ref, ociname.StrictValidation)
	if err != nil || !validRepositorySegments(parsed.RepositoryStr()) {
		return false
	}

	if parsed.Name() == ref {
		return true
	}

	base, _, hasDigest := strings.Cut(ref, "@")
	if !hasDigest {
		return false
	}

	tagged, err := ociname.NewTag(base, ociname.StrictValidation)

	return err == nil && tagged.Name() == base
}

// ValidTaggedRef reports whether ref is a fully qualified, tagged image
// reference. Exact reproduction rejects names that the parser would otherwise
// expand with Docker Hub defaults.
func ValidTaggedRef(ref string) bool {
	ref = explicitRegistryName(ref)
	parsed, err := ociname.NewTag(ref, ociname.StrictValidation)

	return err == nil && parsed.Name() == ref && validRepositorySegments(parsed.RepositoryStr())
}

// ValidRepositoryName requires an explicit, lowercase, tag-free OCI repository.
func ValidRepositoryName(value string) bool {
	value = explicitRegistryName(value)
	parsed, err := ociname.NewRepository(value, ociname.StrictValidation)

	return err == nil && parsed.Name() == value && value == strings.ToLower(value) && validRepositorySegments(parsed.RepositoryStr())
}

// The name library rewrites Docker Hub's documented host to its index alias.
// Accept both explicit hosts, without permitting implicit registry expansion.
func explicitRegistryName(ref string) string {
	if strings.HasPrefix(ref, "docker.io/") {
		return "index.docker.io/" + strings.TrimPrefix(ref, "docker.io/")
	}

	return ref
}

// ValidateManifestTags validates the complete publication set before any writes.
func ValidateManifestTags(image string, tags []string) error {
	if !ValidRepositoryName(image) || len(tags) == 0 {
		return fmt.Errorf("manifest requires a canonical image repository and tags: %w", errs.ErrValidation)
	}

	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !ValidTaggedRef(tag) || StripTag(tag) != image || seen[tag] {
			return fmt.Errorf("manifest tags must be unique tagged references under the image repository: %w", errs.ErrValidation)
		}

		seen[tag] = true
	}

	return nil
}

// OCITagComponent is the unanchored regex fragment for a single OCI tag
// component: the value after `:` in a reference, and equally a promotion stage
// name (which is used verbatim as that tag). It is the single source of the
// tag charset — an alphanumeric/underscore lead followed by up to 127 more of
// `[A-Za-z0-9._-]` — so the ledger's composed ref patterns and the stage-name
// validator embed THIS constant rather than re-spelling the class. Kept
// unanchored so callers can splice it into a larger pattern; the anchored
// standalone form is ValidOCITagComponent.
const OCITagComponent = `[A-Za-z0-9_][A-Za-z0-9._-]{0,127}`

//nolint:gochecknoglobals // shared immutable compiled grammar.
var ociTagRE = regexp.MustCompile("^" + OCITagComponent + "$")

// ValidOCITagComponent reports whether s is a single valid OCI tag component
// (also the rule for a promotion stage name).
func ValidOCITagComponent(s string) bool {
	return ociTagRE.MatchString(s)
}

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

	parsed, err := ociname.ParseReference(name, ociname.WeakValidation)
	if err != nil {
		return "", fmt.Errorf("invalid image repository name %q: %w: %w", ref, err, errs.ErrUsage)
	}

	if !validRepositorySegments(parsed.Context().RepositoryStr()) {
		return "", fmt.Errorf("image repository name contains an empty or relative path segment: %s: %w", ref, errs.ErrUsage)
	}

	last := name[strings.LastIndex(name, "/")+1:]
	if last == "" {
		return "", fmt.Errorf("image repository name is empty after removing tag: %s: %w", ref, errs.ErrUsage)
	}

	return name, nil
}

// CanonicalImageRef removes docker:// and canonicalizes digest refs by dropping
// any tag before @sha256:..., matching Buildah/Skopeo's digest-ref expectation.
//
//nolint:cyclop // one refusal per malformed shape (empty, newline, transport, double digest, bad parse), then canonicalise. Flat guards.
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

	parsed, err := ociname.ParseReference(ref, ociname.WeakValidation)
	if err != nil {
		return "", fmt.Errorf("invalid image ref %q: %w: %w", ref, err, errs.ErrUsage)
	}

	if !validRepositorySegments(parsed.Context().RepositoryStr()) {
		return "", fmt.Errorf("image ref contains an empty or relative path segment: %s: %w", ref, errs.ErrUsage)
	}

	repository, digest, hasDigest := strings.Cut(ref, "@")
	if !hasDigest {
		return ref, nil
	}

	if repository == "" || digest == "" {
		return "", fmt.Errorf("image digest ref must include both name and digest: %s: %w", ref, errs.ErrUsage)
	}

	repository, err = ImageNameWithoutTag(repository)
	if err != nil {
		return "", err
	}

	return repository + "@" + digest, nil
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

// RegistryHost returns the registry authority from either an HTTP(S) URL, a
// bare host, or a fully qualified OCI repository path.
//
//nolint:cyclop // accepts both a URL and a bare OCI repository, each with its own refusals — two flat parse paths rather than nested logic.
func RegistryHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, " \t\n\r") {
		return "", fmt.Errorf("registry host is empty or contains whitespace: %w", errs.ErrUsage)
	}

	// Credentials in the authority are refused before any branch below can
	// quote the input back. Every refusal here used to echo the raw value, so
	// `https://user:TOKEN@registry` -- a plausible way to misconfigure a server
	// URL -- put the token into the error and from there into a CI log. The
	// message states the rule instead of the value.
	if strings.Contains(registryAuthority(value), "@") {
		return "", fmt.Errorf("registry host must not carry credentials (userinfo); supply them through the registry auth inputs: %w", errs.ErrUsage)
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return "", fmt.Errorf("invalid registry URL %q: %w", raw, errs.ErrUsage)
		}

		return parsed.Host, nil
	}

	value = strings.Trim(value, "/")
	if strings.Contains(value, "/") {
		repository, err := ociname.NewRepository(value, ociname.StrictValidation)
		if err != nil {
			return "", fmt.Errorf("invalid registry repository %q: %w: %w", raw, err, errs.ErrUsage)
		}

		return repository.RegistryStr(), nil
	}

	registry, err := ociname.NewRegistry(value, ociname.StrictValidation)
	if err != nil {
		return "", fmt.Errorf("invalid registry host %q: %w: %w", raw, err, errs.ErrUsage)
	}

	return registry.RegistryStr(), nil
}

// registryAuthority returns the authority part of a registry URL, bare host or
// repository path: after any scheme and before the first path separator.
func registryAuthority(value string) string {
	if _, rest, found := strings.Cut(value, "://"); found {
		value = rest
	}

	authority, _, _ := strings.Cut(value, "/")

	return authority
}

func validRepositorySegments(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}

	return true
}
