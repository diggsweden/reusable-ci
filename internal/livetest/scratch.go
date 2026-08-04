// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// This suite creates and destroys its own fixtures through the forge's public
// API. That is the environment's rule and it is the right one: the lab supplies
// what needs lab infrastructure — the instances, the trust, the credentials,
// the runners — and a consumer that can hold a token has no business asking it
// for a repository it could make itself.
//
// Everything here therefore speaks raw HTTP, never the adapters. These calls
// build the world a scenario acts on; if they went through the code under test,
// a broken adapter would produce a broken fixture and the scenario would agree
// with it.

// defaultBranch is the branch every scratch repository is created with, on both
// forges, so a scenario never has to ask which one it got.
const defaultBranch = "main"

// ScratchRepo returns the name of a scratch repository for a scenario, always
// inside the namespace this suite declared it owns.
func ScratchRepo(scenario string) string { return ResourcePrefix + scenario }

// NewScratchRepo brings an empty repository into existence at a known state and
// registers its teardown.
//
// It deletes first. A previous run that died between create and cleanup would
// otherwise leave a repository whose contents no scenario planned, and "start
// from empty" would be a lie told once and believed forever after.
func NewScratchRepo(tb TB, target Target, scenario string) string {
	tb.Helper()
	requireAccepted(tb, target)

	repo := ScratchRepo(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := DeleteScratchRepo(ctx, target, repo); err != nil {
		tb.Fatalf("livetest: reset %s/%s before mutation: %v", target.Owner, repo, err)
	}

	if err := createRepo(ctx, target, repo); err != nil {
		tb.Fatalf("livetest: create %s/%s: %v", target.Owner, repo, err)
	}

	tb.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()

		// Errorf, not Fatalf: cleanup runs after the scenario, and a leaked
		// repository must be visible in the result rather than swallowed.
		if err := DeleteScratchRepo(cleanupCtx, target, repo); err != nil {
			tb.Errorf("livetest: cleanup %s/%s: %v", target.Owner, repo, err)
		}
	})

	return repo
}

// PrepareTag gives a scratch repository a commit and a tag to release from.
//
// Neither adapter sends a target commitish: CreateRelease passes only tag_name
// (GitLab) / TagName (Forgejo), so both assume the tag already exists. That is
// correct for how releases are actually cut — CI tags a commit, then releases
// it — but it means an empty scratch repo cannot be released from, and a
// scenario that skipped this would fail for a fixture reason and read like a
// product bug.
func PrepareTag(tb TB, target Target, repo, tag string) {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := prepareTag(ctx, target, repo, tag); err != nil {
		tb.Fatalf("livetest: prepare %s/%s@%s: %v", target.Owner, repo, tag, err)
	}
}

func prepareTag(ctx context.Context, target Target, repo, tag string) error {
	switch target.Kind {
	case provider.PlatformForgejo:
		// auto_init already left a commit on main, so only the tag is missing.
		body := map[string]any{"tag_name": tag, "target": defaultBranch}
		endpoint := target.BaseURL() + "/api/v1/repos/" + url.PathEscape(target.Owner) + "/" +
			url.PathEscape(repo) + "/tags"

		return discard(ctx, target, http.MethodPost, endpoint, body, http.StatusCreated)

	case provider.PlatformGitLab:
		id, err := gitLabProjectID(ctx, target, repo)
		if err != nil {
			return err
		}

		if id == "" {
			return fmt.Errorf("scratch project %q vanished before tagging: %w", repo, errs.ErrValidation)
		}

		// The project is created bare on purpose: initialize_with_readme is an
		// async worker, so a tag issued right after creation can race it. The
		// first API commit to an empty repo creates the default branch
		// synchronously, which is the deterministic path.
		commit := map[string]any{
			"branch":         defaultBranch,
			"content":        "livetest scratch fixture\n",
			"commit_message": "livetest: seed a commit to tag",
		}
		files := target.BaseURL() + "/api/v4/projects/" + id + "/repository/files/README.md"

		if err := discard(ctx, target, http.MethodPost, files, commit, http.StatusCreated); err != nil {
			return err
		}

		tagBody := map[string]any{"tag_name": tag, "ref": defaultBranch}
		tags := target.BaseURL() + "/api/v4/projects/" + id + "/repository/tags"

		return discard(ctx, target, http.MethodPost, tags, tagBody, http.StatusCreated)

	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return fmt.Errorf("no tag preparation for platform %q: %w", target.Kind, errs.ErrUnsupported)
}

// DeleteScratchRepo removes a scratch repository. Absent is success, so it is
// safe as both a pre-run reset and a teardown.
func DeleteScratchRepo(ctx context.Context, target Target, repo string) error {
	if !strings.HasPrefix(repo, ResourcePrefix) {
		return fmt.Errorf("refusing to delete %q: outside the %q namespace this suite owns: %w", repo, ResourcePrefix, errs.ErrValidation)
	}

	switch target.Kind {
	case provider.PlatformForgejo:
		endpoint := target.BaseURL() + "/api/v1/repos/" + url.PathEscape(target.Owner) + "/" + url.PathEscape(repo)

		return discard(ctx, target, http.MethodDelete, endpoint, nil, http.StatusNoContent, http.StatusNotFound)

	case provider.PlatformGitLab:
		id, err := gitLabProjectID(ctx, target, repo)
		if err != nil {
			return err
		}

		if id == "" {
			return nil
		}

		// GitLab's DELETE only *schedules* deletion: the project is renamed and
		// a redirect route keeps answering on the old path, so the next create
		// collides with a corpse. permanently_remove finishes the job, and it
		// needs the renamed full path, which is why the id is resolved first.
		endpoint := target.BaseURL() + "/api/v4/projects/" + id
		if err := discard(ctx, target, http.MethodDelete, endpoint, nil,
			http.StatusAccepted, http.StatusNoContent, http.StatusNotFound); err != nil {
			return err
		}

		return gitLabPurge(ctx, target, id)

	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return fmt.Errorf("no scratch-repo cleanup for platform %q: %w", target.Kind, errs.ErrUnsupported)
}

func createRepo(ctx context.Context, target Target, repo string) error {
	switch target.Kind {
	case provider.PlatformForgejo:
		body := map[string]any{"name": repo, "auto_init": true, "default_branch": defaultBranch, "private": false}

		return discard(ctx, target, http.MethodPost, target.BaseURL()+"/api/v1/user/repos", body, http.StatusCreated)

	case provider.PlatformGitLab:
		// Bare on purpose. initialize_with_readme is an async worker, so a
		// commit issued straight after creation can 400 with "branch does not
		// exist" whenever sidekiq is busy. PrepareTag makes the first commit
		// itself, which creates the default branch synchronously.
		body := map[string]any{"name": repo, "path": repo, "visibility": "public"}

		return discard(ctx, target, http.MethodPost, target.BaseURL()+"/api/v4/projects", body, http.StatusCreated)

	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return fmt.Errorf("no scratch-repo creation for platform %q: %w", target.Kind, errs.ErrUnsupported)
}

func gitLabProjectID(ctx context.Context, target Target, repo string) (string, error) {
	endpoint := target.BaseURL() + "/api/v4/projects/" + url.PathEscape(target.Owner+"/"+repo)

	var project struct {
		ID int64 `json:"id"`
	}

	status, err := decode(ctx, target, http.MethodGet, endpoint, nil, &project,
		http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", err
	}

	if status == http.StatusNotFound || project.ID == 0 {
		return "", nil
	}

	return strconv.FormatInt(project.ID, 10), nil
}

func gitLabPurge(ctx context.Context, target Target, id string) error {
	var project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	}

	status, err := decode(ctx, target, http.MethodGet, target.BaseURL()+"/api/v4/projects/"+id, nil, &project,
		http.StatusOK, http.StatusNotFound)
	if err != nil || status == http.StatusNotFound || project.PathWithNamespace == "" {
		return err
	}

	endpoint := target.BaseURL() + "/api/v4/projects/" + id +
		"?permanently_remove=true&full_path=" + url.QueryEscape(project.PathWithNamespace)

	return discard(ctx, target, http.MethodDelete, endpoint, nil,
		http.StatusAccepted, http.StatusNoContent, http.StatusNotFound)
}

// discard performs a request and asserts the status, keeping no body.
func discard(ctx context.Context, target Target, method, endpoint string, body any, accept ...int) error {
	_, err := decode(ctx, target, method, endpoint, body, nil, accept...)

	return err
}

// decode performs an authenticated request, checks the status against the
// accepted set, and optionally decodes the body. It returns the status so a
// caller can distinguish "found" from "absent" without a second call.
func decode(ctx context.Context, target Target, method, endpoint string, body, into any, accept ...int) (int, error) {
	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode request: %w", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	authorize(req, target)

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, redact(endpoint), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !accepted(resp.StatusCode, accept) {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return resp.StatusCode, fmt.Errorf("%s %s: HTTP %d: %s: %w", method, redact(endpoint), resp.StatusCode, detail, errs.ErrValidation)
	}

	if into != nil && resp.StatusCode != http.StatusNotFound {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", redact(endpoint), err)
		}
	}

	return resp.StatusCode, nil
}

func accepted(status int, accept []int) bool {
	for _, want := range accept {
		if status == want {
			return true
		}
	}

	return false
}

func authorize(req *http.Request, target Target) {
	switch target.Kind {
	case provider.PlatformForgejo:
		req.Header.Set("Authorization", "token "+target.Token)
	case provider.PlatformGitLab:
		req.Header.Set("PRIVATE-TOKEN", target.Token)
	case provider.PlatformGitHub, provider.PlatformLocal:
	}
}

// redact keeps a failed-request message useful without echoing a query string,
// which on these APIs can carry a token.
func redact(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "<unparseable endpoint>"
	}

	parsed.RawQuery = ""

	return parsed.String()
}
