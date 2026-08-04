// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TaggedRef is a container reference split into the parts a forge's package or
// registry API needs: the registry host, the repository path beneath it, and
// the mutable tag.
//
// Splitting a ref is syntax and is identical on every forge; *resolving* the
// result to an API target is not, which is why the TagDeleter role is per-forge
// while this parse is shared. Keeping it here means the rules that must hold
// everywhere — above all the refusal of a digest-pinned ref — are stated once.
// A per-adapter copy of that refusal is the kind of duplication that drifts,
// and the failure mode when it does is destroying a promoted release image.
type TaggedRef struct {
	// Host is the registry host, port included when the ref carried one.
	Host string

	// Path is the repository path beneath the host ("owner/name", or deeper
	// where a forge nests images below a project).
	Path string

	// Tag is the mutable tag. Never a digest: ParseTaggedRef refuses those.
	Tag string
}

// Owner is the first path segment — the user or organisation that owns the
// repository, which is the scope a package API is keyed on.
func (r TaggedRef) Owner() string {
	owner, _, _ := strings.Cut(r.Path, "/")

	return owner
}

// Name is the path beneath the owner, slash-joined when a forge nests images
// below the repository.
func (r TaggedRef) Name() string {
	_, name, _ := strings.Cut(r.Path, "/")

	return name
}

// ParseTaggedRef splits <host>/<owner>/<name>[/<more>...]:<tag>.
//
// It refuses, in this order:
//
//   - a digest-pinned ref, which names a manifest rather than a tag. Deleting
//     by digest under the shared-digest promotion model destroys the promoted
//     image, so this must never be silently treated as a tag;
//   - a ref with no tag, so a caller cannot accidentally address a whole
//     repository where a tag was meant;
//   - a tag outside the OCI tag charset. Adapters splice the tag into a request
//     path, and while each escapes it, refusing `../` and friends at the parse
//     is the guard that does not depend on every call site remembering to;
//   - a path with fewer than two segments or an empty/dot segment.
func ParseTaggedRef(ref string) (TaggedRef, error) {
	if strings.Contains(ref, "@") {
		return TaggedRef{}, fmt.Errorf("container ref %q is digest-pinned, not a tag: %w", ref, errs.ErrUsage)
	}

	if strings.ContainsAny(ref, "\n\r") {
		return TaggedRef{}, fmt.Errorf("container ref %q contains a line break: %w", ref, errs.ErrUsage)
	}

	// A ':' at or before the final '/' is a host:port, not a tag separator.
	slash := strings.LastIndex(ref, "/")

	colon := strings.LastIndex(ref, ":")
	if colon <= slash {
		return TaggedRef{}, fmt.Errorf("container ref %q carries no tag: %w", ref, errs.ErrUsage)
	}

	tag := ref[colon+1:]
	if !ValidOCITagComponent(tag) {
		return TaggedRef{}, fmt.Errorf("container ref %q has an invalid tag %q: %w", ref, tag, errs.ErrUsage)
	}

	host, path, found := strings.Cut(ref[:colon], "/")
	if !found || host == "" {
		return TaggedRef{}, fmt.Errorf("container ref %q is not <host>/<owner>/<name>:<tag>: %w", ref, errs.ErrUsage)
	}

	if err := validRepositoryPath(ref, path); err != nil {
		return TaggedRef{}, err
	}

	return TaggedRef{Host: host, Path: path, Tag: tag}, nil
}

// validRepositoryPath requires an owner and a name, and rejects empty or
// dot segments — both because they are not valid repository paths and because
// they are how a traversal would be smuggled into a request path.
func validRepositoryPath(ref, path string) error {
	segments := strings.Split(path, "/")

	const ownerAndName = 2
	if len(segments) < ownerAndName {
		return fmt.Errorf("container ref %q is not <host>/<owner>/<name>:<tag>: %w", ref, errs.ErrUsage)
	}

	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("container ref %q has an empty or relative path segment: %w", ref, errs.ErrUsage)
		}
	}

	return nil
}
