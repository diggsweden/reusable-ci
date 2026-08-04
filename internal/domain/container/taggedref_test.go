// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestParseTaggedRef_Accepts(t *testing.T) {
	for _, tc := range []struct {
		name             string
		ref              string
		host, path, tag  string
		owner, imageName string
	}{
		{
			name: "owner and name", ref: "codeberg.org/itiquette/gommitlint:staging-v1.2.3",
			host: "codeberg.org", path: "itiquette/gommitlint", tag: "staging-v1.2.3",
			owner: "itiquette", imageName: "gommitlint",
		},
		{
			name: "image nested below the project", ref: "registry.example.com/group/proj/app:v1.0.0",
			host: "registry.example.com", path: "group/proj/app", tag: "v1.0.0",
			owner: "group", imageName: "proj/app",
		},
		{
			// The ':' here belongs to the host, and the one after it to the tag.
			name: "host with a port", ref: "localhost:5000/owner/name:latest",
			host: "localhost:5000", path: "owner/name", tag: "latest",
			owner: "owner", imageName: "name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := container.ParseTaggedRef(tc.ref)
			if err != nil {
				t.Fatalf("ParseTaggedRef(%q) = %v", tc.ref, err)
			}

			for _, got := range []struct{ what, have, want string }{
				{"host", parsed.Host, tc.host},
				{"path", parsed.Path, tc.path},
				{"tag", parsed.Tag, tc.tag},
				{"owner", parsed.Owner(), tc.owner},
				{"name", parsed.Name(), tc.imageName},
			} {
				if got.have != got.want {
					t.Errorf("%s = %q, want %q", got.what, got.have, got.want)
				}
			}
		})
	}
}

func TestParseTaggedRef_Refuses(t *testing.T) {
	for _, tc := range []struct{ name, ref, why string }{
		{
			name: "digest-pinned",
			ref:  "codeberg.org/owner/name@sha256:" + strings.Repeat("a", 64),
			why:  "a digest names a manifest; deleting it destroys the promoted image",
		},
		{
			name: "digest-pinned with a tag too",
			ref:  "codeberg.org/owner/name:v1@sha256:" + strings.Repeat("a", 64),
			why:  "the digest must win over the tag rather than the tag being parsed out",
		},
		{name: "no tag", ref: "codeberg.org/owner/name", why: "addresses a repository, not a tag"},
		{name: "no path", ref: "codeberg.org:v1", why: "no owner or name"},
		{name: "only one path segment", ref: "codeberg.org/name:v1", why: "no owner"},
		{
			name: "traversal in the tag", ref: "codeberg.org/owner/name:../../etc",
			why: "the tag is spliced into a request path",
		},
		{name: "empty tag", ref: "codeberg.org/owner/name:", why: "nothing to address"},
		{name: "traversal in the path", ref: "codeberg.org/owner/../other:v1", why: "escapes the owner"},
		{name: "empty path segment", ref: "codeberg.org/owner//name:v1", why: "not a repository path"},
		{name: "line break", ref: "codeberg.org/owner/name:v1\nGET /", why: "request splitting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := container.ParseTaggedRef(tc.ref)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("ParseTaggedRef(%q) = (%+v, %v), want ErrUsage — %s", tc.ref, parsed, err, tc.why)
			}
		})
	}
}
