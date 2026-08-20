// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitlabserver"
)

// ListContainerPackageVersions was uncovered. Snapshot cleanup lists the
// versions, decides which are stale and deletes those tags -- so a list
// that is short by a page leaves stale images behind, and one that
// includes a neighbouring image's tags deletes from the wrong repository.

const glTagsEndpoint = glRegistryRepos + "/42/tags"

func newPackagesProvider(srv *fakegitlabserver.Server) *gitlab.Provider {
	return &gitlab.Provider{
		APIBaseOverride: srv.URL(),
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "t"}),
	}
}

// queryValue reads a single query parameter from a recorded request.
func queryValue(req fakegitlabserver.Request, key string) string {
	if values := req.Query[key]; len(values) > 0 {
		return values[0]
	}

	return ""
}

// tagPage renders one page of the registry tags response.
func tagPage(names ...string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf(`{"name":%q}`, name))
	}

	return "[" + strings.Join(quoted, ",") + "]"
}

func TestListContainerPackageVersions_ReturnsEveryTag(t *testing.T) {
	srv := fakegitlabserver.New(t)

	srv.OnGet(glRegistryRepos, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `[{"id":42,"path":"itiquette/repo"}]`}
	})
	srv.OnGet(glTagsEndpoint, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: tagPage("staging-v1.2.3", "v1.2.3", "latest")}
	})

	got, err := newPackagesProvider(srv).ListContainerPackageVersions(context.Background(), "itiquette", "repo")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"staging-v1.2.3", "v1.2.3", "latest"}
	if !slices.Equal(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

// TestListContainerPackageVersions_PagesUntilShort covers the paging
// rule. A full page means there may be more; cleanup that stopped after
// the first page would leave every snapshot beyond it behind, and the
// repository would grow without bound while the run reported success.
func TestListContainerPackageVersions_PagesUntilShort(t *testing.T) {
	srv := fakegitlabserver.New(t)

	srv.OnGet(glRegistryRepos, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `[{"id":42,"path":"itiquette/repo"}]`}
	})

	// A full first page (the API's cap), then a short second one.
	const pageSize = 100

	first := make([]string, 0, pageSize)
	for i := range pageSize {
		first = append(first, "snapshot-"+strconv.Itoa(i))
	}

	srv.OnGet(glTagsEndpoint, func(req fakegitlabserver.Request) fakegitlabserver.Response {
		switch queryValue(req, "page") {
		case "1":
			return fakegitlabserver.Response{Status: 200, Body: tagPage(first...)}
		case "2":
			return fakegitlabserver.Response{Status: 200, Body: tagPage("last-one")}
		default:
			return fakegitlabserver.Response{Status: 200, Body: `[]`}
		}
	})

	got, err := newPackagesProvider(srv).ListContainerPackageVersions(context.Background(), "itiquette", "repo")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != pageSize+1 {
		t.Fatalf("tags = %d, want %d -- the second page was not fetched", len(got), pageSize+1)
	}

	if got[len(got)-1] != "last-one" {
		t.Errorf("last tag = %q, want the one only page 2 carries", got[len(got)-1])
	}

	// A short page ends the walk: asking for a third costs a round trip
	// to learn nothing.
	for _, req := range srv.Requests() {
		if queryValue(req, "page") == "3" {
			t.Error("a third page was requested after a short one")
		}
	}
}

// TestListContainerPackageVersions_NoSuchImageIsEmptyNotAnError covers
// the case cleanup meets most often: an image that was never pushed.
// Treating that as a failure would abort a run that has nothing to do.
func TestListContainerPackageVersions_NoSuchImageIsEmptyNotAnError(t *testing.T) {
	srv := fakegitlabserver.New(t)

	srv.OnGet(glRegistryRepos, func(fakegitlabserver.Request) fakegitlabserver.Response {
		// The project exists and holds two images, neither of them this
		// one. "itiquette/re" is deliberately a PREFIX of the requested
		// "itiquette/repo": the resolver matches paths exactly, and a
		// prefix match here would list -- and later delete -- tags from
		// a neighbouring image.
		return fakegitlabserver.Response{Status: 200,
			Body: `[{"id":7,"path":"itiquette/other"},{"id":8,"path":"itiquette/re"}]`}
	})

	got, err := newPackagesProvider(srv).ListContainerPackageVersions(context.Background(), "itiquette", "repo")
	if err != nil {
		t.Fatalf("an image that was never pushed was reported as an error: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("tags = %v, want none -- these belong to a different image", got)
	}

	// No tag listing happened at all. Not "none from repository 7":
	// resolving to nothing must stop before any tags endpoint, including
	// the one for id 0 that an unresolved repository would produce.
	for _, req := range srv.Requests() {
		if strings.Contains(req.Path, "/tags") {
			t.Errorf("tags were listed for an image that does not exist: %s", req.Path)
		}
	}
}

func TestListContainerPackageVersions_Refusals(t *testing.T) {
	srv := fakegitlabserver.New(t)
	provider := newPackagesProvider(srv)

	for _, tc := range []struct{ name, owner, image string }{
		{name: "no owner", owner: "", image: "repo"},
		{name: "no name", owner: "itiquette", image: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := provider.ListContainerPackageVersions(context.Background(), tc.owner, tc.image)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}
		})
	}

	if len(srv.Requests()) != 0 {
		t.Errorf("requests were sent despite both calls being refused: %d", len(srv.Requests()))
	}
}

// TestListContainerPackageVersions_MalformedBodyIsAnError keeps a
// garbled response from reading as "no tags", which cleanup would take
// as nothing to do.
func TestListContainerPackageVersions_MalformedBodyIsAnError(t *testing.T) {
	srv := fakegitlabserver.New(t)

	srv.OnGet(glRegistryRepos, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `[{"id":42,"path":"itiquette/repo"}]`}
	})
	srv.OnGet(glTagsEndpoint, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `{"not":"an array"}`}
	})

	_, err := newPackagesProvider(srv).ListContainerPackageVersions(context.Background(), "itiquette", "repo")
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}
}
