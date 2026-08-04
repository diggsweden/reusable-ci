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
	"os"
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
		// A failing in-runner scenario reports only a conclusion, and the job log
		// that would explain it lives in a repository this cleanup is about to
		// delete. Forgejo's actions API does not serve those logs by any stable
		// route, so the only way to read one is to still have the repository —
		// hence an explicit opt-out, used while diagnosing and never in a normal
		// run. Deliberately not a flag: it must be awkward enough that nobody
		// leaves it on. The next run starts by deleting the repository anyway, so
		// what is kept is one generation, not a growing pile.
		if os.Getenv("RC_LIVE_KEEP_SCRATCH") == "1" {
			tb.Logf("livetest: keeping %s/%s for inspection (RC_LIVE_KEEP_SCRATCH=1)", target.Owner, repo)

			return
		}

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
		if err := commitFile(ctx, target, repo, "README.md",
			"livetest: seed a commit to tag", "livetest scratch fixture\n"); err != nil {
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
		// Forgejo packages belong to the *owner*, not the repository, so
		// deleting the repo leaves every container image it published behind as
		// an orphan. Nothing later refers to them and nothing else collects
		// them, so they accumulate silently on the lab.
		if err := deleteForgejoPackages(ctx, target, repo); err != nil {
			return err
		}

		endpoint := target.BaseURL() + "/api/v1/repos/" + url.PathEscape(target.Owner) + "/" + url.PathEscape(repo)

		return discard(ctx, target, http.MethodDelete, endpoint, nil, http.StatusNoContent, http.StatusNotFound)

	case provider.PlatformGitLab:
		return deleteGitLabProject(ctx, target, repo)

	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return fmt.Errorf("no scratch-repo cleanup for platform %q: %w", target.Kind, errs.ErrUnsupported)
}

// deleteForgejoPackages removes the container packages a scratch repository
// published. They are addressed by owner and package name, and the scenarios
// name their images after the repository, so the repository name is the handle.
func deleteForgejoPackages(ctx context.Context, target Target, repo string) error {
	var packages []struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	list := target.BaseURL() + "/api/v1/packages/" + url.PathEscape(target.Owner) + "?limit=100"
	if _, err := decode(ctx, target, http.MethodGet, list, nil, &packages, http.StatusOK, http.StatusNotFound); err != nil {
		return err
	}

	for _, pkg := range packages {
		// Only this repository's packages, and only inside the namespace this
		// suite owns: another consumer's packages live on the same owner.
		if pkg.Name != repo || !strings.HasPrefix(pkg.Name, ResourcePrefix) {
			continue
		}

		endpoint := target.BaseURL() + "/api/v1/packages/" + url.PathEscape(target.Owner) + "/" +
			url.PathEscape(pkg.Type) + "/" + url.PathEscape(pkg.Name) + "/" + url.PathEscape(pkg.Version)

		if err := discard(ctx, target, http.MethodDelete, endpoint, nil,
			http.StatusNoContent, http.StatusNotFound); err != nil {
			return err
		}
	}

	return nil
}

// emptyGitLabRegistry removes what accumulates in a project's registry.
//
// Two GitLab behaviours shape this. A project whose registry path is occupied
// cannot be deleted at all — deletion renames the project, and the bundled
// registry refuses to rename an occupied path ("Cannot rename project, the
// container registry path rename validation failed"). And deleting a container
// *repository* is asynchronous: the API answers 202 and a scheduled Sidekiq job
// does the work, which on a stock instance is minutes away, not seconds.
//
// So tags are deleted directly, which is synchronous and is what actually
// accumulates, and the repository delete is issued as a best effort without
// waiting on it. Blocking on the drain would make the suite unusably slow for a
// behaviour it cannot influence.
func emptyGitLabRegistry(ctx context.Context, target Target, projectID string) error {
	list := target.BaseURL() + "/api/v4/projects/" + projectID + "/registry/repositories?per_page=100"

	repositories, err := gitLabRegistryRepositoryIDs(ctx, target, list)
	if err != nil || len(repositories) == 0 {
		return err
	}

	for _, id := range repositories {
		base := target.BaseURL() + "/api/v4/projects/" + projectID +
			"/registry/repositories/" + strconv.FormatInt(id, 10)

		var tags []struct {
			Name string `json:"name"`
		}

		if _, err := decode(ctx, target, http.MethodGet, base+"/tags?per_page=100", nil, &tags,
			http.StatusOK, http.StatusNotFound); err != nil {
			return err
		}

		for _, tag := range tags {
			if err := discard(ctx, target, http.MethodDelete, base+"/tags/"+url.PathEscape(tag.Name), nil,
				http.StatusOK, http.StatusNoContent, http.StatusAccepted, http.StatusNotFound); err != nil {
				return err
			}
		}

		if err := discard(ctx, target, http.MethodDelete, base, nil,
			http.StatusAccepted, http.StatusNoContent, http.StatusNotFound); err != nil {
			return err
		}
	}

	return nil
}

func gitLabRegistryRepositoryIDs(ctx context.Context, target Target, list string) ([]int64, error) {
	var repositories []struct {
		ID int64 `json:"id"`
	}

	if _, err := decode(ctx, target, http.MethodGet, list, nil, &repositories,
		http.StatusOK, http.StatusNotFound); err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.ID)
	}

	return ids, nil
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

// deleteGitLabProject removes a scratch project, in the order GitLab requires.
func deleteGitLabProject(ctx context.Context, target Target, repo string) error {
	id, marked, err := gitLabProject(ctx, target, repo)
	if err != nil {
		return err
	}

	if id == "" {
		return nil
	}

	// An already-marked project refuses a second plain DELETE with 400
	// "Project has already been marked for deletion" — it is renamed and
	// waiting, so the only thing left to do is finish the job.
	if marked {
		return gitLabPurge(ctx, target, id)
	}

	// A project with images in its registry cannot be deleted at all:
	// deletion renames the project, and GitLab refuses to rename a project
	// whose container registry path is in use — "Cannot rename project, the
	// container registry path rename validation failed". So the registry is
	// emptied first, or teardown fails and the project is stuck.
	if err := emptyGitLabRegistry(ctx, target, id); err != nil {
		return err
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
}

// gitLabProject resolves a path to the numeric id and reports whether the
// project is already marked for deletion. Addressing by id avoids GitLab's
// redirect routes, which keep answering on the path of a project that was
// deleted and recreated; the marked flag decides whether a plain DELETE is
// still the right call or would 400.
func gitLabProject(ctx context.Context, target Target, repo string) (string, bool, error) {
	endpoint := target.BaseURL() + "/api/v4/projects/" + url.PathEscape(target.Owner+"/"+repo)

	var project struct {
		ID                int64   `json:"id"`
		MarkedForDeletion *string `json:"marked_for_deletion_on"`
	}

	status, err := decode(ctx, target, http.MethodGet, endpoint, nil, &project,
		http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", false, err
	}

	if status == http.StatusNotFound || project.ID == 0 {
		return "", false, nil
	}

	return strconv.FormatInt(project.ID, 10), project.MarkedForDeletion != nil, nil
}

func gitLabProjectID(ctx context.Context, target Target, repo string) (string, error) {
	id, _, err := gitLabProject(ctx, target, repo)

	return id, err
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
		return "<unparsable endpoint>"
	}

	parsed.RawQuery = ""

	return parsed.String()
}

// CommitFile adds or replaces one file on the default branch, so a scenario can
// move the branch on from where a tag points.
//
// Exported because "the tag and the branch head are the same commit" makes a
// whole class of ref assertions vacuous: a checkout that ignored the requested
// ref and took the default branch would land on the same commit and look
// correct. Advancing the branch is what gives those assertions something to be
// wrong about.
func CommitFile(tb TB, target Target, repo, path, message, content string) {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := commitFile(ctx, target, repo, path, message, content); err != nil {
		tb.Fatalf("livetest: commit %s to %s/%s: %v", path, target.Owner, repo, err)
	}
}

// TagCommitSHA asks the forge which commit a tag points at.
//
// The oracle for "did the checkout land on the right ref". Asking the resulting
// clone instead would be asking git about what git just did, and it is fragile
// besides: a clone made at a branch does not necessarily carry the tag object,
// so the comparison fails to run rather than failing to match.
func TagCommitSHA(tb TB, target Target, repo, tag string) string {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch target.Kind {
	case provider.PlatformGitLab:
		var payload struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}

		endpoint := target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/repository/tags/" + url.PathEscape(tag)
		if _, err := decode(ctx, target, http.MethodGet, endpoint, nil, &payload, http.StatusOK); err != nil {
			tb.Fatalf("livetest: read tag %s: %v", tag, err)
		}

		return payload.Commit.ID
	case provider.PlatformForgejo, provider.PlatformGitHub, provider.PlatformLocal:
		var payload struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}

		endpoint := target.BaseURL() + "/api/v1/repos/" + url.PathEscape(target.Owner) + "/" +
			url.PathEscape(repo) + "/tags/" + url.PathEscape(tag)
		if _, err := decode(ctx, target, http.MethodGet, endpoint, nil, &payload, http.StatusOK); err != nil {
			tb.Fatalf("livetest: read tag %s: %v", tag, err)
		}

		return payload.Commit.SHA
	}

	return ""
}

// NewScratchRepoUnique is NewScratchRepo for scenarios whose identity is bound
// to the repository path.
//
// The ordinary scratch repository is reused by name: deleted and recreated at a
// fixed path, so a scenario always starts from a known state. For OIDC that is
// exactly wrong, and GitLab enforces it. Deleting a project records its path in
// `burned_project_routes`, and any project later created at a burned path is
// refused an ID token -- `id_token_burned_project_path`, which fails the job
// before the script runs, so it reads as a workflow fault rather than a naming
// one. The rule exists for a good reason: without it, recreating a project at a
// deleted project's path would inherit its identity with any relying party that
// trusts the path claim.
//
// So the path must be new every time, not merely unlikely to collide. It is
// derived from the clock rather than from the run id, which sounds equivalent
// and is not: the run id belongs to the CONTRACT, so every scenario run against
// one contract would share a path, and the second run would find what the first
// one burned. That is precisely how this was discovered.
//
// The cost is accepted deliberately: there is no delete-first step to lean on,
// so a run that dies between create and cleanup leaves a repository behind, and
// every run adds a burned route on the forge. Both stay inside the namespace
// this suite owns, and a lab is disposable -- which is the trade being made.
func NewScratchRepoUnique(tb TB, target Target, scenario string) string {
	tb.Helper()

	// Lowercase alphanumerics only. This name reaches the URLs these helpers
	// build and the prefix check deciding what the suite may delete, so it
	// should not depend on validation happening somewhere else.
	unique := strconv.FormatInt(time.Now().UnixNano(), 36)

	// Reclaim earlier generations first. A fixed-name scratch repository is
	// reclaimed by the delete-before-create in NewScratchRepo; a unique one has
	// nothing to reclaim it, so without this every run of an OIDC scenario would
	// leave its repository behind forever. Same discipline, adapted: what cannot
	// be reused can at least be swept.
	sweepScratchRepos(tb, target, ScratchRepo(scenario)+"-")

	return NewScratchRepo(tb, target, scenario+"-"+unique)
}

// sweepScratchRepos deletes repositories whose name starts with prefix.
//
// Failures are reported and not fatal: a leftover repository is untidy, and
// failing the run that noticed it would be worse than the untidiness. The prefix
// always starts with the namespace this suite owns, and DeleteScratchRepo
// refuses anything outside it regardless.
func sweepScratchRepos(tb TB, target Target, prefix string) {
	tb.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, name := range scratchRepoNames(ctx, target, prefix) {
		if err := DeleteScratchRepo(ctx, target, name); err != nil {
			tb.Logf("livetest: could not sweep %s/%s: %v", target.Owner, name, err)
		}
	}
}

// scratchRepoNames lists this owner's repositories matching prefix.
func scratchRepoNames(ctx context.Context, target Target, prefix string) []string {
	var names []string

	switch target.Kind {
	case provider.PlatformGitLab:
		var projects []struct {
			Path string `json:"path"`
		}

		endpoint := target.BaseURL() + "/api/v4/projects?owned=true&per_page=100"
		if _, err := decode(ctx, target, http.MethodGet, endpoint, nil, &projects, http.StatusOK); err != nil {
			return nil
		}

		for _, project := range projects {
			if strings.HasPrefix(project.Path, prefix) {
				names = append(names, project.Path)
			}
		}
	case provider.PlatformForgejo, provider.PlatformGitHub, provider.PlatformLocal:
		var repos []struct {
			Name string `json:"name"`
		}

		endpoint := target.BaseURL() + "/api/v1/user/repos?limit=100"
		if _, err := decode(ctx, target, http.MethodGet, endpoint, nil, &repos, http.StatusOK); err != nil {
			return nil
		}

		for _, repo := range repos {
			if strings.HasPrefix(repo.Name, prefix) {
				names = append(names, repo.Name)
			}
		}
	}

	return names
}
