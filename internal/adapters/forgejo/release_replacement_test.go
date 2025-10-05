// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

// TestPublishRelease_ReplacementFailureGuarantees states what a same-basename
// replacement promises when one of its two API calls fails.
//
// Replacing an asset is an upload and a delete, and Forgejo offers no way to do
// both at once, so one of them happens first and the other can fail after it.
// Deleting first means a failed upload leaves the release with neither the old
// asset nor the new one, and a published artifact is gone. Uploading first can
// at worst leave two assets of the same name, which is recoverable and, more
// importantly, reportable. This pins that choice from the outside: the old asset
// survives an upload failure, and a delete that fails is named in the error
// rather than leaving a duplicate nobody hears about.
//
// What it does not claim is that Forgejo ends up in either state: these are the
// calls the adapter makes and the errors it returns, not observed server state.
func TestPublishRelease_ReplacementFailureGuarantees(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		// fail decides which call the server refuses.
		failUpload bool
		failDelete bool
		wantCalls  []string
		wantErr    string
	}{
		{
			name:       "a failed upload leaves the published asset in place",
			failUpload: true,
			wantCalls: []string{
				"GET /api/v1/repos/itiquette/repo/releases/tags/v1.2.3",
				"GET /api/v1/repos/itiquette/repo/releases/99/assets",
				"POST /api/v1/repos/itiquette/repo/releases/99/assets",
			},
			wantErr: "upload asset",
		},
		{
			name:       "a failed replacement delete is reported, not left silent",
			failDelete: true,
			wantCalls: []string{
				"GET /api/v1/repos/itiquette/repo/releases/tags/v1.2.3",
				"GET /api/v1/repos/itiquette/repo/releases/99/assets",
				"POST /api/v1/repos/itiquette/repo/releases/99/assets",
				"DELETE /api/v1/repos/itiquette/repo/releases/99/assets/1",
			},
			wantErr: `delete replaced release asset "asset.tgz"`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			asset := filepath.Join(dir, "asset.tgz")
			require.NoError(t, os.WriteFile(asset, []byte("replacement bytes\n"), 0o600))

			notes := filepath.Join(dir, "notes.md")
			require.NoError(t, os.WriteFile(notes, []byte("release notes\n"), 0o600))

			var calls []string

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/tags/v1.2.3":
					_, _ = w.Write([]byte(`{"id":99,"tag_name":"v1.2.3","assets":[{"id":1,"name":"asset.tgz"}]}`))
				case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99":
					_, _ = w.Write([]byte(`{"id":99,"tag_name":"v1.2.3"}`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets":
					_, _ = w.Write([]byte(`[{"id":1,"name":"asset.tgz"}]`))
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets":
					if testCase.failUpload {
						http.Error(w, "upload refused", http.StatusInsufficientStorage)

						return
					}

					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":7,"name":"asset.tgz"}`))
				case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets/1":
					if testCase.failDelete {
						http.Error(w, "delete refused", http.StatusConflict)

						return
					}

					w.WriteHeader(http.StatusNoContent)
				default:
					http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
				}
			})

			err := newProvider(handler).PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
				Tag: "v1.2.3", Name: "repo v1.2.3", NotesFile: notes, Assets: []string{asset},
			})

			require.Error(t, err, "a replacement that did not complete must not report success")
			require.ErrorContains(t, err, testCase.wantErr)
			require.Equal(t, testCase.wantCalls, calls, "the adapter stops at the failed call")
			require.NotContains(t, calls, "PATCH /api/v1/repos/itiquette/repo/releases/99",
				"a reconciliation that failed must leave the previous release description in place")

			if testCase.failUpload {
				require.False(t, slices.ContainsFunc(calls, func(call string) bool {
					return call == "DELETE /api/v1/repos/itiquette/repo/releases/99/assets/1"
				}), "the published asset must not be deleted when its replacement never uploaded")
			}
		})
	}
}

// attachmentStore is release 99's attachment list kept as state, with one
// injectable failure for the next upload or the next delete.
type attachmentStore struct {
	mu         sync.Mutex
	stored     []storedAttachment
	nextID     int64
	failUpload bool
	failDelete bool
}

type storedAttachment struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	content string
}

const storeAssets = "/api/v1/repos/itiquette/repo/releases/99/assets"

func (store *attachmentStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	store.mu.Lock()
	defer store.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/tags/v1.2.3",
		r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99":
		_, _ = w.Write([]byte(`{"id":99,"tag_name":"v1.2.3"}`))
	case r.Method == http.MethodGet && r.URL.Path == storeAssets:
		writeJSON(w, store.stored)
	case r.Method == http.MethodPost && r.URL.Path == storeAssets:
		store.upload(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, storeAssets+"/"):
		store.delete(w, strings.TrimPrefix(r.URL.Path, storeAssets+"/"))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
	}
}

func (store *attachmentStore) upload(w http.ResponseWriter, r *http.Request) {
	if store.failUpload {
		store.failUpload = false

		http.Error(w, "upload refused", http.StatusInsufficientStorage)

		return
	}

	file, header, err := r.FormFile("attachment")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	store.nextID++
	uploaded := storedAttachment{ID: store.nextID, Name: header.Filename, content: string(content)}
	store.stored = append(store.stored, uploaded)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, uploaded)
}

func (store *attachmentStore) delete(w http.ResponseWriter, rawID string) {
	if store.failDelete {
		store.failDelete = false

		http.Error(w, "delete refused", http.StatusConflict)

		return
	}

	id, err := strconv.ParseInt(rawID, 10, 64)
	index := slices.IndexFunc(store.stored, func(a storedAttachment) bool { return a.ID == id })

	if err != nil || index < 0 {
		http.Error(w, "no such asset", http.StatusNotFound)

		return
	}

	store.stored = slices.Delete(store.stored, index, index+1)

	w.WriteHeader(http.StatusNoContent)
}

// state describes each stored attachment as id:content, in upload order.
func (store *attachmentStore) state() []string {
	store.mu.Lock()
	defer store.mu.Unlock()

	described := make([]string, 0, len(store.stored))
	for _, a := range store.stored {
		described = append(described, strconv.FormatInt(a.ID, 10)+":"+a.content)
	}

	return described
}

// TestPublishRelease_RetryAfterAReplacementFailureConverges keeps the release's
// attachments as state and retries the same publish after each failure. A
// failed upload leaves the old asset; a failed delete leaves the old and the new
// asset under one name. From either state the retry must end with exactly one
// asset of that name, holding the replacement's bytes, because every run lists
// the attachments again and removes each same-name one only after its own
// upload succeeded.
func TestPublishRelease_RetryAfterAReplacementFailureConverges(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                   string
		failUpload, failDelete bool
		wantAfterFailure       []string
	}{
		{name: "after a failed upload", failUpload: true, wantAfterFailure: []string{"1:published bytes\n"}},
		{name: "after a failed delete", failDelete: true, wantAfterFailure: []string{"1:published bytes\n", "2:replacement bytes\n"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			asset := filepath.Join(dir, "asset.tgz")
			require.NoError(t, os.WriteFile(asset, []byte("replacement bytes\n"), 0o600))

			notes := filepath.Join(dir, "notes.md")
			require.NoError(t, os.WriteFile(notes, []byte("release notes\n"), 0o600))

			store := &attachmentStore{
				stored:     []storedAttachment{{ID: 1, Name: "asset.tgz", content: "published bytes\n"}},
				nextID:     1,
				failUpload: testCase.failUpload,
				failDelete: testCase.failDelete,
			}

			publish := func() error {
				return newProvider(store).PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
					Tag: "v1.2.3", Name: "repo v1.2.3", NotesFile: notes, Assets: []string{asset},
				})
			}

			require.Error(t, publish(), "the injected failure must fail the first run")
			require.Equal(t, testCase.wantAfterFailure, store.state(), "the failure leaves its documented intermediate state")

			require.NoError(t, publish(), "the retry must succeed")

			final := store.state()
			require.Len(t, final, 1, "the retry must leave exactly one asset of the name: %q", final)
			require.True(t, strings.HasSuffix(final[0], ":replacement bytes\n"), "the surviving asset must be the replacement: %q", final)
		})
	}
}
