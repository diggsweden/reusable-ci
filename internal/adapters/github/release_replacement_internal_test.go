// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// replacementAPI answers the requests of one asset replacement on release 42:
// known-good asset 7 named app.bin, staged upload 9. Each "METHOD path" has a
// queue of statuses for its successive calls; a missing or zero entry succeeds.
// The first rename of asset 7 is the backup, so its name is kept even when
// that rename is refused.
type replacementAPI struct {
	t        *testing.T
	statuses map[string][]int

	mu         sync.Mutex
	trace      []string
	stagedName string
	backupName string
}

func (api *replacementAPI) provider() *Provider {
	return &Provider{
		APIBaseOverride: "https://github.invalid",
		Env: func(key string) string {
			if key == "GITHUB_REPOSITORY" {
				return "o/r"
			}

			return ""
		},
		HTTPClient: &http.Client{Transport: contractTransport(api.roundTrip)},
	}
}

func (api *replacementAPI) roundTrip(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.Path

	api.mu.Lock()
	defer api.mu.Unlock()

	api.trace = append(api.trace, key)

	if key == "PATCH /repos/o/r/releases/assets/7" && api.backupName == "" {
		var rename struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(req.Body).Decode(&rename); err != nil {
			api.t.Errorf("rename body: %v", err)
		}

		api.backupName = rename.Name
	}

	status := 0
	if queue := api.statuses[key]; len(queue) > 0 {
		status, api.statuses[key] = queue[0], queue[1:]
	}

	if status != 0 {
		return contractResponse(req, status, fmt.Sprintf(`{"message":"refused %d"}`, status)), nil
	}

	switch key {
	case "GET /repos/o/r/releases/tags/v1":
		return contractResponse(req, http.StatusOK, `{"id":42}`), nil
	case "GET /repos/o/r/releases/42/assets":
		return contractResponse(req, http.StatusOK, `[{"id":7,"name":"app.bin"}]`), nil
	case "POST /repos/o/r/releases/42/assets":
		api.stagedName = req.URL.Query().Get("name")

		return contractResponse(req, http.StatusCreated,
			fmt.Sprintf(`{"id":9,"name":%q,"size":3,"digest":"sha256:%x"}`, api.stagedName, sha256.Sum256([]byte("new")))), nil
	case "PATCH /repos/o/r/releases/assets/7":
		return contractResponse(req, http.StatusOK, `{"id":7}`), nil
	case "PATCH /repos/o/r/releases/assets/9":
		return contractResponse(req, http.StatusOK, `{"id":9}`), nil
	case "DELETE /repos/o/r/releases/assets/7", "DELETE /repos/o/r/releases/assets/9":
		return contractResponse(req, http.StatusNoContent, ""), nil
	default:
		api.t.Errorf("unexpected request %s", key)

		return contractResponse(req, http.StatusBadRequest, `{}`), nil
	}
}

// TestUploadReleaseAsset_EachReplacementFailureHasItsTraceAndRecoveryNames
// completes the replacement matrix beside the promotion-failure test: the
// backup rename refused with the staged copy removed or kept, a promotion
// whose restore succeeds but whose staged cleanup fails, and a backup that
// cannot be deleted after a successful promotion. Each case pins the whole
// request sequence, the error classes, and the asset ids and names an
// operator needs to recover; stage and backup names are the random ones the
// adapter chose.
func TestUploadReleaseAsset_EachReplacementFailureHasItsTraceAndRecoveryNames(t *testing.T) {
	t.Parallel()

	const (
		lookup  = "GET /repos/o/r/releases/tags/v1"
		list    = "GET /repos/o/r/releases/42/assets"
		upload  = "POST /repos/o/r/releases/42/assets"
		backup  = "PATCH /repos/o/r/releases/assets/7"
		promote = "PATCH /repos/o/r/releases/assets/9"
		dropNew = "DELETE /repos/o/r/releases/assets/9"
		dropOld = "DELETE /repos/o/r/releases/assets/7"
	)

	tests := []struct {
		name     string
		statuses map[string][]int
		trace    []string
		classes  []error
		message  string
	}{
		{
			name:     "backup refused, staged copy removed",
			statuses: map[string][]int{backup: {http.StatusForbidden}},
			trace:    []string{lookup, list, upload, backup, dropNew},
			classes:  []error{errs.ErrPermissionDenied},
			message:  `stage existing asset "app.bin" as backup: refused 403: {perm}; known-good asset id 7 may be named "app.bin" or "{backup}"`,
		},
		{
			name:     "backup refused, staged copy kept",
			statuses: map[string][]int{backup: {http.StatusForbidden}, dropNew: {http.StatusServiceUnavailable}},
			trace:    []string{lookup, list, upload, backup, dropNew},
			classes:  []error{errs.ErrPermissionDenied, errs.ErrDependencyUnavailable},
			message: `stage existing asset "app.bin" as backup: refused 403: {perm}; cleanup failed: refused 503: {unavail}; ` +
				`known-good asset id 7 may be named "app.bin" or "{backup}"; staged asset id 9 at "{staged}" remains`,
		},
		{
			name:     "promotion refused, restored, staged copy kept",
			statuses: map[string][]int{promote: {http.StatusServiceUnavailable}, dropNew: {http.StatusForbidden}},
			trace:    []string{lookup, list, upload, backup, promote, backup, dropNew},
			classes:  []error{errs.ErrDependencyUnavailable, errs.ErrPermissionDenied},
			message: `promote staged asset "app.bin": refused 503: {unavail}; cleanup failed: refused 403: {perm}; ` +
				`original restored, staged asset id 9 at "{staged}" remains`,
		},
		{
			name:     "promoted, backup not deleted",
			statuses: map[string][]int{dropOld: {http.StatusServiceUnavailable}},
			trace:    []string{lookup, list, upload, backup, promote, dropOld},
			classes:  []error{errs.ErrDependencyUnavailable},
			message:  `delete replaced asset backup "{backup}": refused 503: {unavail}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "app.bin")
			require.NoError(t, os.WriteFile(file, []byte("new"), 0o600))

			api := &replacementAPI{t: t, statuses: tc.statuses}
			err := api.provider().UploadReleaseAsset(t.Context(), "v1", file)

			for _, class := range tc.classes {
				require.ErrorIs(t, err, class)
			}

			require.Equal(t, tc.trace, api.trace)
			require.Contains(t, api.stagedName, "app.bin.reusable-ci-upload-")
			require.Contains(t, api.backupName, "app.bin.reusable-ci-backup-")

			want := strings.NewReplacer(
				"{perm}", errs.ErrPermissionDenied.Error(), "{unavail}", errs.ErrDependencyUnavailable.Error(),
				"{backup}", api.backupName, "{staged}", api.stagedName,
			).Replace(tc.message)
			require.Equal(t, want, err.Error())
		})
	}
}

// TestPublishRelease_RefusesTwoReleasesOnOneTag: GitHub allows several draft
// releases with the same tag name. Editing either would be a guess, so the
// publication stops after the listing and names both releases.
func TestPublishRelease_RefusesTwoReleasesOnOneTag(t *testing.T) {
	t.Parallel()

	var trace []string

	p := &Provider{
		APIBaseOverride: "https://github.invalid",
		HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
			trace = append(trace, req.Method+" "+req.URL.Path)

			return contractResponse(req, http.StatusOK,
				`[{"id":1,"tag_name":"v1","draft":true},{"id":2,"tag_name":"v0"},{"id":3,"tag_name":"v1","draft":true}]`), nil
		})},
	}

	err := p.PublishRelease(t.Context(), "o/r", provider.ReleaseSpec{Tag: "v1"})
	require.ErrorIs(t, err, errs.ErrValidation)
	require.ErrorContains(t, err, `releases 1 and 3 both use tag "v1"`)
	require.Equal(t, []string{"GET /repos/o/r/releases"}, trace)
}
