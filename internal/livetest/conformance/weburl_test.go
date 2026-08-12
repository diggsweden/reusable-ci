// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-UX-1: the links a run summary prints actually resolve.
//
// Every release summary ends with a link to the release and a link to the
// packages page, and until now nothing checked either. A unit test can only
// assert the string matches what the code was written to produce, which is the
// same assumption twice; the forge is the only thing that knows whether
// "/-/releases/v1" is a page or a 404. A dead link in a release summary is
// exactly the kind of defect nobody reports and everybody sees.
//
// These are anonymous requests on purpose. The pages are public, and a link
// printed for a human is only useful if it works without the pipeline's
// credential — so sending one would test something weaker than what is claimed.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestWebURLs_ResolveOnEveryForge(t *testing.T) {
	const tag = "v0.0.1"

	for _, forge := range forgesClaiming(t, claimsReleaseAssets, "releases") {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "weburl")
			slug := livetest.RepoSlug(target, repo)

			adapter := livetest.Provider(t, target, repo)

			urls, ok := adapter.(provider.WebURLBuilder)
			if !ok {
				t.Fatalf("%s does not implement WebURLBuilder, so its summary can only print placeholders", forge)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			// A release has to exist before its page can resolve; both adapters
			// release an existing tag rather than creating one.
			livetest.PrepareTag(t, target, repo, tag)

			creator, ok := adapter.(provider.ReleaseCreator)
			if !ok {
				t.Fatalf("%s does not implement ReleaseCreator", forge)
			}

			if err := creator.CreateRelease(ctx, slug, provider.ReleaseSpec{Tag: tag, Name: "PAR-UX-1"}); err != nil {
				t.Fatalf("%s create release: %v", forge, err)
			}

			for _, page := range []struct{ what, url string }{
				{"release", urls.ReleaseWebURL(target.BaseURL(), slug, tag)},
				{"packages", urls.PackagesWebURL(target.BaseURL(), slug)},
			} {
				assertPageResolves(t, ctx, forge, page.what, page.url)
			}
		})
	}
}

// assertPageResolves fails unless the URL serves a page. A redirect is not
// accepted: on both forges an unauthenticated redirect is how a missing or
// private page is served, so following it would turn a dead link into a pass.
func assertPageResolves(t *testing.T, ctx context.Context, forge provider.ForgeAPI, what, url string) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", url, err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s page %s: %v", forge, what, url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("%s %s page %s returned HTTP %d — the summary would print a dead link",
			forge, what, url, resp.StatusCode)
	}
}
