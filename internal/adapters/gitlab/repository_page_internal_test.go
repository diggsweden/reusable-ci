// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"encoding/json"
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRegistryPageBoundary_FindsExactSecondPageForListAndDelete(t *testing.T) {
	t.Parallel()

	for _, deleting := range []bool{false, true} {
		var calls []string

		p := &Provider{APIBaseOverride: "https://gitlab.invalid/api/v4", Env: func(string) string { return "" }, HTTPClient: &http.Client{Transport: guardTransport(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.Method+" "+req.URL.RequestURI())
			body := `[]`

			switch {
			case strings.HasSuffix(req.URL.Path, "/registry/repositories"):
				if req.URL.Query().Get("page") == "1" {
					batch := make([]registryRepository, 100)
					for i := range batch {
						batch[i] = registryRepository{ID: int64(i + 1), Path: fmt.Sprintf("group/other-%d", i)}
					}

					data, err := json.Marshal(batch)
					require.NoError(t, err)

					body = string(data)
				} else {
					body = `[{"id":701,"path":"group/project"}]`
				}
			case strings.HasSuffix(req.URL.Path, "/701/tags"):
				body = `[{"name":"v1"}]`
			case req.Method == http.MethodDelete:
				require.True(t, strings.HasSuffix(req.URL.Path, "/701/tags/staging-v1"))
			default:
				t.Errorf("unexpected request %s", req.URL)
			}

			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}}
		if deleting {
			require.NoError(t, p.DeleteTag(t.Context(), "registry.invalid/group/project:staging-v1"))
		} else {
			versions, err := p.ListContainerPackageVersions(t.Context(), "group", "project")
			require.NoError(t, err)
			require.Equal(t, []string{"v1"}, versions)
		}

		require.Len(t, calls, 3)
		require.Contains(t, calls[0], "page=1")
		require.Contains(t, calls[1], "page=2")
	}
}

func TestGitLabIdentityBoundary_RejectsBlankAttestedProject(t *testing.T) {
	t.Parallel()

	for _, url := range []string{" ", "\t", "\r\n"} {
		p := &Provider{Env: func(key string) string {
			switch key {
			case "GITLAB_CI":
				return "true"
			case "CI_PROJECT_URL":
				return url
			case "CI_SERVER_URL":
				return "https://gitlab.invalid"
			}

			return ""
		}}
		id, err := p.ResolveKeylessIdentity()
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Equal(t, provider.KeylessIdentity{}, id)
	}
}
