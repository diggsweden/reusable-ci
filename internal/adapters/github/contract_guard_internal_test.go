// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	gogithub "github.com/google/go-github/v76/github"
	"github.com/stretchr/testify/require"
)

type contractTransport func(*http.Request) (*http.Response, error)

func (f contractTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func contractResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestArtifactExactName_ScansAllPagesAndRefusesDuplicates(t *testing.T) {
	t.Parallel()

	for _, duplicate := range []bool{false, true} {
		calls := 0
		client := gogithub.NewClient(&http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
			calls++

			body := `{"artifacts":[{"id":7,"name":"wanted"}]}`
			if calls == 2 && !duplicate {
				body = `{"artifacts":[{"id":8,"name":"other"}]}`
			}

			resp := contractResponse(req, 200, body)
			if calls == 1 {
				resp.Header.Set("Link", `<https://github.invalid/repos/o/r/actions/runs/1/artifacts?page=2>; rel="next"`)
			}

			return resp, nil
		})})
		client.BaseURL, _ = url.Parse("https://github.invalid/")

		id, err := findArtifactID(t.Context(), client, "o", "r", 1, "wanted")
		if duplicate {
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Zero(t, id)
		} else {
			require.NoError(t, err)
			require.Equal(t, int64(7), id)
		}

		require.Equal(t, 2, calls)
	}
}

func TestArtifactFinalize_RequiresConfirmedIdentity(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{}`, `{"ok":false,"artifactId":"7"}`, `{"ok":true}`, `{"ok":true,"artifactId":" "}`, `{"ok":true,"artifactId":"7"}`} {
		p := &Provider{HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) { return contractResponse(req, 200, body), nil })}}

		id, err := p.finalizeArtifact(t.Context(), uploadContext{resultsURL: "https://github.invalid", token: "synthetic"}, "artifact", zipArchive{})
		if body == `{"ok":true,"artifactId":"7"}` {
			require.NoError(t, err)
			require.Equal(t, "7", id)
		} else {
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Empty(t, id)
		}
	}
}

func TestReleaseRollback_ReportsRetainedRecoveryObjects(t *testing.T) {
	t.Parallel()

	for _, restoreFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(restoreFails), func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "app.bin")
			require.NoError(t, os.WriteFile(file, []byte("new"), 0o600))

			oldName, stageName := "app.bin", ""
			renames, deletes := 0, 0
			p := &Provider{APIBaseOverride: "https://github.invalid", Env: func(key string) string {
				if key == "GITHUB_REPOSITORY" {
					return "o/r"
				}

				return ""
			}}
			p.HTTPClient = &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
				switch req.Method + " " + req.URL.Path {
				case "GET /repos/o/r/releases/tags/v1":
					return contractResponse(req, 200, `{"id":42}`), nil
				case "GET /repos/o/r/releases/42/assets":
					return contractResponse(req, 200, `[{"id":7,"name":"app.bin"}]`), nil
				case "POST /repos/o/r/releases/42/assets":
					stageName = req.URL.Query().Get("name")
					body := fmt.Sprintf(`{"id":9,"name":%q,"size":3,"digest":"sha256:%x"}`, stageName, sha256.Sum256([]byte("new")))

					return contractResponse(req, 201, body), nil
				case "PATCH /repos/o/r/releases/assets/7":
					renames++

					var data struct {
						Name string `json:"name"`
					}
					require.NoError(t, json.NewDecoder(req.Body).Decode(&data))

					if renames == 2 && restoreFails {
						return contractResponse(req, 403, `{"message":"restore refused"}`), nil
					}

					oldName = data.Name

					return contractResponse(req, 200, `{"id":7}`), nil
				case "PATCH /repos/o/r/releases/assets/9":
					return contractResponse(req, 503, `{"message":"promote refused"}`), nil
				case "DELETE /repos/o/r/releases/assets/9":
					deletes++

					return contractResponse(req, 204, ""), nil
				default:
					t.Errorf("unexpected %s %s", req.Method, req.URL.Path)

					return contractResponse(req, 400, `{}`), nil
				}
			})}
			err := p.UploadReleaseAsset(t.Context(), "v1", file)
			require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
			require.Equal(t, 2, renames)

			if restoreFails {
				require.ErrorIs(t, err, errs.ErrPermissionDenied)
				require.Zero(t, deletes)
				require.Contains(t, oldName, ".reusable-ci-backup-")
				require.Contains(t, err.Error(), oldName)
				require.Contains(t, err.Error(), stageName)
			} else {
				require.Equal(t, 1, deletes)
				require.Equal(t, "app.bin", oldName)
			}
		})
	}
}
