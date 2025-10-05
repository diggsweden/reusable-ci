// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type pagingTransport func(*http.Request) (*http.Response, error)

func (f pagingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestListings_RefuseAServerThatIgnoresThePageParameter covers the listing
// loops that stopped only on a short page. A server answering every request
// with the same full page used to be followed until it stopped, each page
// collected again: 301 requests and 30,000 duplicate links in the audit
// probe. A repeated item now ends the listing as malformed after the second
// page, and a server that pages properly still returns every item once.
func TestListings_RefuseAServerThatIgnoresThePageParameter(t *testing.T) {
	t.Parallel()

	fullPage := func(prefix string) string {
		items := make([]string, 0, gitlabPageSize)
		for i := range gitlabPageSize {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"%s-%d","path":"group/project/%s%d"}`, i+1, prefix, i+1, prefix, i+1))
		}

		return "[" + strings.Join(items, ",") + "]"
	}

	serve := func(pages map[string]string, ignorePage bool) (*Provider, *int) {
		requests := 0

		return &Provider{APIBaseOverride: "https://gl.invalid", HTTPClient: &http.Client{Transport: pagingTransport(func(r *http.Request) (*http.Response, error) {
			requests++

			page := r.URL.Query().Get("page")
			if ignorePage {
				page = "1"
			}

			body, ok := pages[page]
			if !ok {
				body = "[]"
			}

			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}}, &requests
	}

	for name, list := range map[string]func(*Provider) (int, error){
		"release links": func(p *Provider) (int, error) {
			links, err := p.listReleaseLinks(context.Background(), "https://gl.invalid/api/v4/projects/1/releases/v1/assets/links", "v1", nil)

			return len(links), err
		},
		"registry repositories": func(p *Provider) (int, error) {
			repositories, err := p.listRegistryRepositories(context.Background(), "https://gl.invalid", nil, "group/project")

			return len(repositories), err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pages := map[string]string{"1": fullPage("a"), "2": `[{"id":999,"name":"last","path":"group/project/last"}]`}

			p, requests := serve(pages, false)

			count, err := list(p)
			if err != nil || count != gitlabPageSize+1 || *requests != 2 {
				t.Fatalf("paging server: count=%d requests=%d err=%v, want every item once over two requests", count, *requests, err)
			}

			p, requests = serve(pages, true)

			count, err = list(p)
			if !errors.Is(err, errs.ErrMalformedInput) || count != 0 || *requests != 2 {
				t.Fatalf("page-ignoring server: count=%d requests=%d err=%v, want a malformed refusal after the repeated page", count, *requests, err)
			}
		})
	}
}
