// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// recordingRegistry is the package's in-process registry with every request
// recorded, so a test can tell reads from writes.
type recordingRegistry struct {
	mu       sync.Mutex
	requests []string
	repo     string
}

func newRecordingRegistry(t *testing.T) *recordingRegistry {
	t.Helper()

	rec := &recordingRegistry{}
	handler := registry.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.requests = append(rec.requests, r.Method+" "+r.URL.Path)
		rec.mu.Unlock()

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	rec.repo = strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	return rec
}

// writes returns every recorded request that could change registry state.
func (r *recordingRegistry) writes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, request := range r.requests {
		if !strings.HasPrefix(request, "GET ") && !strings.HasPrefix(request, "HEAD ") {
			out = append(out, request)
		}
	}

	return out
}

func (r *recordingRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.requests)
}

// TestPublication_RefusalsWriteNothingToTheRegistry drives the public push and
// merge entry points against a registry that records requests. The helper
// tests prove the layout and index guards on their own; these prove the
// adapters consult them before anything is sent: an invalid layout or
// destination reaches no endpoint at all, a malformed digest or foreign tag
// stops before any request, and an index with a duplicate concrete platform
// is refused after reading its children and before writing a tag.
func TestPublication_RefusalsWriteNothingToTheRegistry(t *testing.T) {
	t.Parallel()

	layoutWith := func(t *testing.T, images int) string {
		t.Helper()

		index := v1.ImageIndex(empty.Index)

		for range images {
			img, err := random.Image(64, 1)
			require.NoError(t, err)

			index = mutate.AppendManifests(index, mutate.IndexAddendum{Add: img})
		}

		dir := t.TempDir()
		_, err := layout.Write(dir, index)
		require.NoError(t, err)

		return dir
	}

	for name, images := range map[string]int{"empty layout": 0, "two-image layout": 2} {
		t.Run("push "+name, func(t *testing.T) {
			t.Parallel()

			reg := newRecordingRegistry(t)

			_, err := New().PushLayoutByDigest(t.Context(), layoutWith(t, images), reg.repo)
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Zero(t, reg.count())
		})
	}

	t.Run("push to a malformed destination", func(t *testing.T) {
		t.Parallel()

		reg := newRecordingRegistry(t)

		_, err := New().PushLayoutByDigest(t.Context(), layoutWith(t, 1), reg.repo+"/Not A Repo")
		require.Error(t, err)
		require.Zero(t, reg.count())
	})

	t.Run("merge with a malformed digest", func(t *testing.T) {
		t.Parallel()

		reg := newRecordingRegistry(t)

		err := New().MergeManifest(t.Context(), reg.repo, []string{"zz"}, []string{reg.repo + ":v1"})
		require.Error(t, err)
		require.Zero(t, reg.count())
	})

	t.Run("merge to a tag of another repository", func(t *testing.T) {
		t.Parallel()

		reg := newRecordingRegistry(t)

		err := New().MergeManifest(t.Context(), reg.repo, []string{strings.Repeat("a", 64)}, []string{reg.repo + "-other:v1"})
		require.Error(t, err)
		require.Zero(t, reg.count())
	})

	t.Run("merge of two children claiming one platform", func(t *testing.T) {
		t.Parallel()

		reg := newRecordingRegistry(t)

		digests := make([]string, 0, 2)

		for tag := range 2 {
			img, err := random.Image(64, 1)
			require.NoError(t, err)

			img, err = mutate.ConfigFile(img, &v1.ConfigFile{OS: "linux", Architecture: "amd64"})
			require.NoError(t, err)

			ref := reg.repo + ":child-" + string(rune('a'+tag))
			require.NoError(t, crane.Push(img, ref, crane.Insecure))

			digest, err := img.Digest()
			require.NoError(t, err)

			digests = append(digests, digest.Hex)
		}

		before := reg.writes()

		err := New().MergeManifest(t.Context(), reg.repo, digests, []string{reg.repo + ":v1"})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Equal(t, before, reg.writes(), "the refused merge wrote to the registry")
	})
}
