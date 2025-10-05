// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// pagedAPI answers GET requests from an in-memory table keyed by path and
// page number, adding the Link header go-github reads the next page from.
// Every requested path?query is recorded, so a test can see each page was
// actually asked for.
type pagedAPI struct {
	pages    map[string][]string // path -> JSON body per page, page 1 first
	nextPage map[string]string   // optional path -> forced next page value
	requests []string
}

func (a *pagedAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	a.requests = append(a.requests, req.URL.Path+"?"+req.URL.RawQuery)

	pages := a.pages[req.URL.Path]
	page := 1

	if raw := req.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > len(pages) {
			return nil, fmt.Errorf("paged api: no page %q for %s", raw, req.URL.Path) //nolint:err113 // test double refusal.
		}

		page = parsed
	}

	header := http.Header{"Content-Type": {"application/json"}}

	next := ""
	if page < len(pages) {
		next = strconv.Itoa(page + 1)
	}

	if forced, ok := a.nextPage[req.URL.Path]; ok && page == len(pages) {
		next = forced
	}

	if next != "" {
		header.Set("Link", `<https://api.example.invalid`+req.URL.Path+`?page=`+next+`&per_page=100>; rel="next"`)
	}

	return &http.Response{
		StatusCode: http.StatusOK, Header: header, Request: req,
		Body: io.NopCloser(strings.NewReader(pages[page-1])),
	}, nil
}

func pagedClient(t *testing.T, api *pagedAPI) *gogithub.Client {
	t.Helper()

	client := gogithub.NewClient(&http.Client{Transport: api})

	base, err := url.Parse("https://api.example.invalid/")
	if err != nil {
		t.Fatal(err)
	}

	client.BaseURL = base

	return client
}

// TestListPages_CollectsEveryPageAndRefusesOneThatDoesNotAdvance covers the
// page loops with fixtures where the object that matters is on page two: a
// matching artifact, a second artifact with a name already seen on page one,
// and a release. Every fixture used to be a single page, so a loop that
// stopped after the first request passed. A server whose last page links to
// itself is refused after two requests instead of being followed forever.
func TestListPages_CollectsEveryPageAndRefusesOneThatDoesNotAdvance(t *testing.T) {
	t.Parallel()

	const artifactsPath = "/repos/owner/repo/actions/runs/7/artifacts"

	artifactPages := []string{
		`{"total_count":3,"artifacts":[{"id":1,"name":"dist-amd64"},{"id":2,"name":"sbom"}]}`,
		`{"total_count":3,"artifacts":[{"id":3,"name":"dist-arm64"}]}`,
	}
	wantRequests := []string{artifactsPath + "?per_page=100", artifactsPath + "?page=2&per_page=100"}

	t.Run("a pattern match on page two", func(t *testing.T) {
		t.Parallel()

		api := &pagedAPI{pages: map[string][]string{artifactsPath: artifactPages}}

		got, err := listMatchingArtifacts(context.Background(), pagedClient(t, api), "owner", "repo", 7, "dist-*")
		if err != nil {
			t.Fatal(err)
		}

		if want := []artifactRef{{name: "dist-amd64", id: 1}, {name: "dist-arm64", id: 3}}; !slices.Equal(got, want) {
			t.Errorf("matches = %+v, want %+v", got, want)
		}

		if !slices.Equal(api.requests, wantRequests) {
			t.Errorf("requests = %q, want %q", api.requests, wantRequests)
		}
	})

	t.Run("an exact name only on page two", func(t *testing.T) {
		t.Parallel()

		api := &pagedAPI{pages: map[string][]string{artifactsPath: artifactPages}}

		id, err := findArtifactID(context.Background(), pagedClient(t, api), "owner", "repo", 7, "dist-arm64")
		if err != nil || id != 3 {
			t.Errorf("id = %d, err = %v; want 3", id, err)
		}
	})

	t.Run("the same name on both pages", func(t *testing.T) {
		t.Parallel()

		api := &pagedAPI{pages: map[string][]string{artifactsPath: {
			`{"total_count":2,"artifacts":[{"id":1,"name":"dist"}]}`,
			`{"total_count":2,"artifacts":[{"id":2,"name":"dist"}]}`,
		}}}

		if _, err := findArtifactID(context.Background(), pagedClient(t, api), "owner", "repo", 7, "dist"); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("err = %v, want the duplicate refused", err)
		}
	})

	t.Run("a release on page two", func(t *testing.T) {
		t.Parallel()

		const releasesPath = "/repos/owner/repo/releases"

		api := &pagedAPI{pages: map[string][]string{releasesPath: {
			`[{"id":10,"tag_name":"v1.0.0-rc.1","prerelease":true}]`,
			`[{"id":11,"tag_name":"v1.0.0"}]`,
		}}}

		releases, err := listAllRepositoryReleases(context.Background(), pagedClient(t, api), "owner", "repo")
		if err != nil {
			t.Fatal(err)
		}

		tags := make([]string, 0, len(releases))
		for _, release := range releases {
			tags = append(tags, release.GetTagName())
		}

		if !slices.Equal(tags, []string{"v1.0.0-rc.1", "v1.0.0"}) || len(api.requests) != 2 {
			t.Errorf("tags = %q after %d requests, want both pages", tags, len(api.requests))
		}
	})

	t.Run("a last page that links to itself", func(t *testing.T) {
		t.Parallel()

		const assetsPath = "/repos/owner/repo/releases/10/assets"

		api := &pagedAPI{
			pages:    map[string][]string{assetsPath: {`[{"id":1,"name":"a"}]`, `[{"id":2,"name":"b"}]`}},
			nextPage: map[string]string{assetsPath: "2"},
		}

		_, err := listAllReleaseAssets(context.Background(), pagedClient(t, api), "owner", "repo", 10)
		if !errors.Is(err, errs.ErrMalformedInput) || len(api.requests) != 2 {
			t.Errorf("err = %v after %d requests, want refusal after 2", err, len(api.requests))
		}
	})
}
