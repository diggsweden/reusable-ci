// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

type releasePolicyTransport func(*http.Request) (*http.Response, error)

func (f releasePolicyTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestReleasePolicy_UnsupportedFlagsRefuseBeforeRequests(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"create", "publish"} {
		for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			calls := 0
			client := &http.Client{Transport: releasePolicyTransport(func(req *http.Request) (*http.Response, error) {
				calls++

				status, body := http.StatusCreated, `{}`
				if req.Method == http.MethodGet {
					status, body = http.StatusNotFound, `{}`
				}

				if strings.HasSuffix(req.URL.Path, "/assets/links") {
					status, body = http.StatusOK, `[]`
				}

				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})}
			adapter := &gitlab.Provider{HTTPClient: client, APIBaseOverride: "https://gitlab.invalid", Env: envFunc(nil)}

			publish := adapter.CreateRelease
			if operation == "publish" {
				publish = adapter.PublishRelease
			}

			err := publish(t.Context(), "fixture/repo", provider.ReleaseSpec{Tag: "v1.2.3", Draft: flags[0], Prerelease: flags[1]})
			if flags[0] || flags[1] {
				if !errors.Is(err, errs.ErrUnsupported) || calls != 0 {
					t.Errorf("%s flags=%v err=%v calls=%d", operation, flags, err, calls)
				}
			} else if err != nil || calls == 0 {
				t.Errorf("supported %s failed: err=%v calls=%d", operation, err, calls)
			}
		}
	}
}
