// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// pushRandomImage writes a random image to repo:tag in an in-process registry
// and returns its digest.
func pushRandomImage(t *testing.T, repo, tag string) string {
	t.Helper()

	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}

	ref := repo + ":" + tag
	if pushErr := crane.Push(img, ref, crane.Insecure); pushErr != nil {
		t.Fatalf("push %s: %v", ref, pushErr)
	}

	digest, err := crane.Digest(ref, crane.Insecure)
	if err != nil {
		t.Fatalf("digest %s: %v", ref, err)
	}

	return digest
}

// localRepo starts an in-process registry and returns the owner, name and full
// repository path addressing it, mimicking a registry running beside a runner.
func localRepo(t *testing.T) (string, string, string) {
	t.Helper()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")

	return host + "/owner", "project-base", host + "/owner/project-base"
}

// TestListContainerPackageVersions proves the lister answers from a plain OCI
// registry, so a local registry can serve the same base-image lifecycle a
// forge package API does.
func TestListContainerPackageVersions(t *testing.T) {
	t.Parallel()

	owner, name, repo := localRepo(t)

	pushRandomImage(t, repo, "aaa")
	pushRandomImage(t, repo, "bbb")

	tags, err := New().ListContainerPackageVersions(context.Background(), owner, name)
	if err != nil {
		t.Fatalf("ListContainerPackageVersions() error = %v", err)
	}

	slices.Sort(tags)

	if len(tags) != 2 || tags[0] != "aaa" || tags[1] != "bbb" {
		t.Errorf("tags = %v, want [aaa bbb]", tags)
	}
}

// TestDeleteTagRemovesOnlyTheNamedTag: the ordinary retention case, where each
// content-addressed base tag has its own manifest.
func TestDeleteTagRemovesOnlyTheNamedTag(t *testing.T) {
	t.Parallel()

	owner, name, repo := localRepo(t)

	pushRandomImage(t, repo, "keep")
	pushRandomImage(t, repo, "prune")

	adapter := New()

	if err := adapter.DeleteTag(context.Background(), repo+":prune"); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}

	tags, err := adapter.ListContainerPackageVersions(context.Background(), owner, name)
	if err != nil {
		t.Fatalf("ListContainerPackageVersions() error = %v", err)
	}

	if len(tags) != 1 || tags[0] != "keep" {
		t.Errorf("tags after delete = %v, want [keep]", tags)
	}
}

// TestDeleteTagRefusesSharedManifest is the reason this adapter exists in this
// shape. The distribution spec has no delete-tag: DELETE removes the manifest
// and every tag on it. A forge package API deletes one version and leaves its
// siblings, so the same call is safe there and destructive here — and staging
// plus final tags routinely share a manifest.
//
// Refusing is the safe direction: a base-image tag is content-addressed and
// has no siblings, so a shared manifest means the caller is not pruning what
// it thinks it is.
func TestDeleteTagRefusesSharedManifest(t *testing.T) {
	t.Parallel()

	owner, name, repo := localRepo(t)

	digest := pushRandomImage(t, repo, "final")

	// A second tag on the same manifest, as promotion produces.
	if err := crane.Tag(repo+":final", "staging", crane.Insecure); err != nil {
		t.Fatalf("tag staging: %v", err)
	}

	adapter := New()

	err := adapter.DeleteTag(context.Background(), repo+":staging")
	if err == nil {
		t.Fatal("DeleteTag() = nil error, want a refusal — deleting would remove the shared manifest")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("error = %v, want errs.ErrValidation", err)
	}

	if !strings.Contains(err.Error(), digest) {
		t.Errorf("error does not name the shared manifest %s: %v", digest, err)
	}

	// Both tags survive the refusal.
	tags, listErr := adapter.ListContainerPackageVersions(context.Background(), owner, name)
	if listErr != nil {
		t.Fatalf("ListContainerPackageVersions() error = %v", listErr)
	}

	if len(tags) != 2 {
		t.Errorf("tags after refusal = %v, want both preserved", tags)
	}
}

// TestDeleteTagIsIdempotent: retention runs repeatedly, and an already-absent
// tag is the desired state rather than an error.
func TestDeleteTagIsIdempotent(t *testing.T) {
	t.Parallel()

	_, _, repo := localRepo(t)

	pushRandomImage(t, repo, "present")

	adapter := New()

	if err := adapter.DeleteTag(context.Background(), repo+":absent"); err != nil {
		t.Errorf("DeleteTag() on a missing tag = %v, want nil", err)
	}
}

// TestDeleteTagRejectsDigestPinnedRef: this adapter deletes tags. A
// digest-pinned ref would remove the manifest itself, which is what every
// caller here is trying to avoid.
func TestDeleteTagRejectsDigestPinnedRef(t *testing.T) {
	t.Parallel()

	_, _, repo := localRepo(t)

	digest := pushRandomImage(t, repo, "only")

	err := New().DeleteTag(context.Background(), repo+"@"+digest)
	if err == nil {
		t.Fatal("DeleteTag() = nil error, want a digest-pinned ref refused")
	}

	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("error = %v, want errs.ErrUsage", err)
	}
}
