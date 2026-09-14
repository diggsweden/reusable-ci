// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

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

// TestListContainerPackageVersions_ReturnsEveryTagInTheRepository proves the lister answers from a plain OCI
// registry, so a local registry can serve the same base-image lifecycle a
// forge package API does.
func TestListContainerPackageVersions_ReturnsEveryTagInTheRepository(t *testing.T) {
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

// TestDeleteTag_RemovesOnlyTheNamedTag: the ordinary retention case, where each
// content-addressed base tag has its own manifest.
func TestDeleteTag_RemovesOnlyTheNamedTag(t *testing.T) {
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

// TestDeleteTag_RefusesSharedManifest is the reason this adapter exists in this
// shape. A forge package API deletes one version and leaves its siblings, but
// registries differ on DELETE by tag: some remove only the tag, some refuse it
// and some remove the manifest and every tag on it. Staging plus final tags
// routinely share a manifest, so the known shared state is refused.
//
// Refusing is the safe direction: a base-image tag is content-addressed and
// has no siblings, so a shared manifest means the caller is not pruning what
// it thinks it is.
func TestDeleteTag_RefusesSharedManifest(t *testing.T) {
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

// TestDeleteTag_ASiblingPublishedAfterThePreflightSurvives interleaves a
// publication between the shared-manifest preflight and the deletion: the
// registry tags the same manifest as "sibling" just before it serves the
// DELETE. The preflight cannot see that tag, so what keeps it is the request
// itself naming the tag. Exactly one DELETE is sent and it addresses
// manifests/prune, never the digest both tags now serve, and the sibling still
// resolves to that manifest afterwards.
func TestDeleteTag_ASiblingPublishedAfterThePreflightSurvives(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		deletes   []string
		manifest  []byte
		mediaType string
	)

	inner := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()

			deletes = append(deletes, r.URL.Path)
			body, contentType := manifest, mediaType
			mu.Unlock()

			put := httptest.NewRequestWithContext(r.Context(), http.MethodPut, "/v2/owner/project-base/manifests/sibling", bytes.NewReader(body))
			put.Header.Set("Content-Type", contentType)

			published := httptest.NewRecorder()
			inner.ServeHTTP(published, put)

			if published.Code != http.StatusCreated {
				t.Errorf("publishing the sibling: status %d: %s", published.Code, published.Body)
			}
		}

		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/owner/project-base"
	digest := pushRandomImage(t, repo, "prune")

	raw, err := crane.Manifest(repo+":prune", crane.Insecure)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	head, err := crane.Head(repo+":prune", crane.Insecure)
	if err != nil {
		t.Fatalf("head manifest: %v", err)
	}

	mu.Lock()
	manifest, mediaType = raw, string(head.MediaType)
	mu.Unlock()

	if err := New().DeleteTag(context.Background(), repo+":prune"); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}

	if want := []string{"/v2/owner/project-base/manifests/prune"}; !slices.Equal(deletes, want) {
		t.Errorf("DELETE requests = %v, want %v", deletes, want)
	}

	if got, err := crane.Digest(repo+":sibling", crane.Insecure); err != nil || got != digest {
		t.Errorf("sibling digest = %q (err %v), want %s", got, err, digest)
	}

	if _, err := crane.Digest(repo+":prune", crane.Insecure); err == nil {
		t.Error("prune still resolves after DeleteTag")
	}
}

// TestDeleteTag_IsIdempotent: retention runs repeatedly, and an already-absent
// tag is the desired state rather than an error — whether it was never there
// or this run removed it a moment ago.
func TestDeleteTag_IsIdempotent(t *testing.T) {
	t.Parallel()

	owner, name, repo := localRepo(t)

	pushRandomImage(t, repo, "present")

	adapter := New()
	ctx := context.Background()

	// Never existed.
	if err := adapter.DeleteTag(ctx, repo+":absent"); err != nil {
		t.Errorf("DeleteTag() on a missing tag = %v, want nil", err)
	}

	// A tag that did exist, removed twice. The repeat is the half that
	// makes this idempotence rather than tolerance of a typo.
	if err := adapter.DeleteTag(ctx, repo+":present"); err != nil {
		t.Fatalf("first DeleteTag() = %v, want nil", err)
	}

	if err := adapter.DeleteTag(ctx, repo+":present"); err != nil {
		t.Errorf("second DeleteTag() = %v, want nil", err)
	}

	// And the tag really is gone: a delete that quietly did nothing would
	// satisfy every error check above.
	tags, err := adapter.ListContainerPackageVersions(ctx, owner, name)
	if err != nil {
		t.Fatalf("ListContainerPackageVersions() error = %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("tags after delete = %v, want none", tags)
	}
}

// TestDeleteTag_RejectsDigestPinnedRef: this adapter deletes tags. A
// digest-pinned ref would remove the manifest itself, which is what every
// caller here is trying to avoid.
func TestDeleteTag_RejectsDigestPinnedRef(t *testing.T) {
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

// TestClassifyRegistryError_MapsEveryClass pins the error class for each way a
// registry call fails. Only 404 and the shared-manifest refusal were exercised,
// through a live in-process registry; the auth, throttling, other 4xx, 5xx,
// status-less transport and malformed-reference branches had no case. A
// reference the library refuses to parse used to be reported as the registry
// being unavailable, which CI treats as worth retrying.
//
// Pagination is not tabled here: go-containerregistry's remote.List follows
// the registry's Link headers, and this adapter adds no paging of its own.
func TestClassifyRegistryError_MapsEveryClass(t *testing.T) {
	t.Parallel()

	_, badName := name.NewRepository("Invalid Repository/With Spaces")
	if badName == nil {
		t.Fatal("fixture: the reference parsed")
	}

	status := func(code int) error {
		return fmt.Errorf("GET /v2/owner/app/tags/list: %w", &transport.Error{StatusCode: code})
	}

	for label, tc := range map[string]struct {
		err  error
		want error
	}{
		"401 unauthorized":       {err: status(http.StatusUnauthorized), want: errs.ErrPermissionDenied},
		"403 forbidden":          {err: status(http.StatusForbidden), want: errs.ErrPermissionDenied},
		"404 not found":          {err: status(http.StatusNotFound), want: errs.ErrMissingInput},
		"429 throttled":          {err: status(http.StatusTooManyRequests), want: errs.ErrRateLimited},
		"400 bad request":        {err: status(http.StatusBadRequest), want: errs.ErrValidation},
		"405 method not allowed": {err: status(http.StatusMethodNotAllowed), want: errs.ErrValidation},
		"500 server error":       {err: status(http.StatusInternalServerError), want: errs.ErrDependencyUnavailable},
		"503 unavailable":        {err: status(http.StatusServiceUnavailable), want: errs.ErrDependencyUnavailable},
		"connection refused":     {err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, want: errs.ErrDependencyUnavailable},
		"malformed reference":    {err: badName, want: errs.ErrUsage},
	} {
		if got := classifyRegistryError(tc.err); !errors.Is(got, tc.want) {
			t.Errorf("%s: class = %v, want %v", label, got, tc.want)
		}
	}
}
