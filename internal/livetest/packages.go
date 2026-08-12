// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// The forge-native package registries, as an oracle.
//
// A package publish is only believable if the forge says the package is there,
// and the two forges answer that question through different APIs: GitLab lists
// packages under the project, the Gitea family lists them under the owner. That
// difference is exactly the kind of knowledge this kit exists to absorb, so a
// scenario can ask "is it published?" without knowing which forge it is talking
// to.
//
// Deliberately not asked through npm or mvn: the tool that published is the
// worst possible witness that publishing worked. The forge's own REST API is the
// oracle, the same rule the rest of this kit follows.

// PublishedPackageVersions returns the versions the forge lists for one package
// in one ecosystem, or an empty slice when the forge knows of none.
//
// ecosystem is spelled the way each forge spells it ("npm", "maven"); both
// forges happen to agree on those two names.
func PublishedPackageVersions(tb TB, target Target, repo, ecosystem, name string) []string {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch target.Forge {
	case provider.ForgeGitLab:
		return gitlabPackageVersions(ctx, tb, target, repo, ecosystem, name)
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		return giteaPackageVersions(ctx, tb, target, ecosystem, name)
	}

	return nil
}

// gitlabPackageVersions reads the project's package list. GitLab scopes packages
// to the project that published them, so the repository is part of the question.
func gitlabPackageVersions(ctx context.Context, tb TB, target Target, repo, ecosystem, name string) []string {
	tb.Helper()

	var packages []struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		PackageType string `json:"package_type"`
	}

	endpoint := target.BaseURL() + "/api/v4/projects/" +
		url.PathEscape(target.Owner+"/"+repo) + "/packages?per_page=100"
	if _, err := decodeQuiet(ctx, target, endpoint, &packages); err != nil {
		tb.Logf("livetest: read gitlab packages: %v", err)

		return nil
	}

	var versions []string

	for _, pkg := range packages {
		if pkg.Name == name && pkg.PackageType == ecosystem {
			versions = append(versions, pkg.Version)
		}
	}

	return versions
}

// giteaPackageVersions reads the owner's package list. The Gitea family scopes
// packages to the owner rather than the repository, so a package published from
// one repository is visible without naming it — which is why repo is not a
// parameter here.
func giteaPackageVersions(ctx context.Context, tb TB, target Target, ecosystem, name string) []string {
	tb.Helper()

	var packages []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
	}

	endpoint := target.BaseURL() + "/api/v1/packages/" + url.PathEscape(target.Owner) +
		"?type=" + url.QueryEscape(ecosystem) + "&limit=100"
	if _, err := decodeQuiet(ctx, target, endpoint, &packages); err != nil {
		tb.Logf("livetest: read forgejo packages: %v", err)

		return nil
	}

	var versions []string

	for _, pkg := range packages {
		if pkg.Name == name {
			versions = append(versions, pkg.Version)
		}
	}

	return versions
}

// DeletePublishedPackage removes one published version, and is a no-op when the
// forge has already forgotten it.
//
// Needed because a package does not belong to the scratch repository that
// published it on every forge: the Gitea family scopes packages to the OWNER, so
// deleting the repository leaves the package behind and the next run inherits
// it. GitLab scopes to the project and is cleaned up by the repository delete,
// but both are swept here so the rule is one rule.
func DeletePublishedPackage(tb TB, target Target, repo, ecosystem, name, version string) {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var endpoint string

	switch target.Forge {
	case provider.ForgeGitLab:
		id := gitlabPackageID(ctx, tb, target, repo, ecosystem, name, version)
		if id == 0 {
			return
		}

		endpoint = target.BaseURL() + "/api/v4/projects/" +
			url.PathEscape(target.Owner+"/"+repo) + "/packages/" + strconv.Itoa(id)
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		endpoint = target.BaseURL() + "/api/v1/packages/" + url.PathEscape(target.Owner) +
			"/" + url.PathEscape(ecosystem) + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
	}

	if endpoint == "" {
		return
	}

	// A package that is already gone is the outcome asked for, so 404 counts.
	if err := discard(ctx, target, http.MethodDelete, endpoint, nil,
		http.StatusNoContent, http.StatusOK, http.StatusNotFound); err != nil {
		tb.Logf("livetest: could not delete %s package %s@%s: %v", ecosystem, name, version, err)
	}
}

// gitlabPackageID finds the numeric id GitLab's delete endpoint needs, or 0.
func gitlabPackageID(ctx context.Context, tb TB, target Target, repo, ecosystem, name, version string) int {
	tb.Helper()

	var packages []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Version     string `json:"version"`
		PackageType string `json:"package_type"`
	}

	endpoint := target.BaseURL() + "/api/v4/projects/" +
		url.PathEscape(target.Owner+"/"+repo) + "/packages?per_page=100"
	if _, err := decodeQuiet(ctx, target, endpoint, &packages); err != nil {
		return 0
	}

	for _, pkg := range packages {
		if pkg.Name == name && pkg.Version == version && pkg.PackageType == ecosystem {
			return pkg.ID
		}
	}

	return 0
}

// NPMPackageName is the name a package must carry to be publishable to this
// forge's npm registry.
//
// This is a real divergence rather than a convenience. The Gitea family serves
// npm under /api/packages/<owner>/npm/ and requires the package to be scoped to
// that owner, so an unscoped name is rejected outright. GitLab's project-level
// registry accepts any name. A fixture that hard-coded one shape would be
// testing a forge it was not running against.
func NPMPackageName(target Target, base string) string {
	if target.Forge == provider.ForgeGitLab {
		return base
	}

	return "@" + target.Owner + "/" + base
}

// SetRepoSecret puts a secret on the scratch repository so a workflow can
// reference it, and returns the expression a job uses to read it.
//
// Needed because a forge's automatic job token is not always enough. Forgejo's
// is documented as having read permission to the repository on push events, and
// it is refused by the package registry — measured, not assumed: a PUT with it
// answers 401/403. So publishing packages from Forgejo Actions requires a token
// carrying write:package, exactly as a real pipeline would have to supply one.
// GitLab needs nothing here: $CI_REGISTRY_PASSWORD / $CI_JOB_TOKEN already carry
// package-write for the project that issued them.
func SetRepoSecret(tb TB, target Target, repo, name, value string) {
	tb.Helper()
	requireAccepted(tb, target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch target.Forge {
	case provider.ForgeGitLab:
		// GitLab CI variables are project variables; not needed by any scenario
		// yet, and adding an unused branch would be untested code.
		tb.Fatalf("livetest: SetRepoSecret is not implemented for %s", target.Forge)
	case provider.ForgeForgejo, provider.ForgeGitHub, provider.ForgeLocal:
		endpoint := target.BaseURL() + "/api/v1/repos/" + url.PathEscape(target.Owner) + "/" +
			url.PathEscape(repo) + "/actions/secrets/" + url.PathEscape(name)
		if err := discard(ctx, target, http.MethodPut, endpoint,
			map[string]any{"data": value},
			http.StatusCreated, http.StatusNoContent, http.StatusOK); err != nil {
			tb.Fatalf("livetest: set secret %s on %s/%s: %v", name, target.Owner, repo, err)
		}
	}
}

// MavenPackageName is the name a forge lists a Maven artifact under.
//
// The two spell coordinates differently: GitLab's package registry names the
// package "<groupId as path>/<artifactId>", while the Gitea family joins them
// with a colon. Asserting one spelling on both would fail against whichever
// forge was not the one it was written for.
func MavenPackageName(target Target, groupID, artifactID string) string {
	if target.Forge == provider.ForgeGitLab {
		return strings.ReplaceAll(groupID, ".", "/") + "/" + artifactID
	}

	return groupID + ":" + artifactID
}
