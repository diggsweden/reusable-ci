// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package container holds pure container-image domain logic: name
// resolution, namespace policy, OCI-label assembly, binary-extract suffix.
package container

import "strings"

// ResolveImageNameInput is the input for ResolveImageName.
type ResolveImageNameInput struct {
	Registry        string // "ghcr.io" / "docker.io" / "registry.gitlab.com" / ...
	ImageName       string // explicit override; if set, returned as-is (after registry-prefix logic)
	Repository      string // "owner/repo" (the source repo's full path)
	RepositoryOwner string // "owner" (used to prefix bare names on docker.io)
	Name            string // optional sub-name from artifacts.yml; collapsed when == repo short name

	// NameSuffix appends to the repository segment of the derived
	// image name (no effect when ImageName is set explicitly).
	// Used by the dev-release flow to push to a namespace
	// distinguishable from production releases: production goes to
	// `ghcr.io/owner/repo`, dev goes to `ghcr.io/owner/repo-dev`.
	// Empty suffix → no change.
	NameSuffix string
}

// ResolveImageName produces the canonical image reference for a build.
//
// Rules:
//
//  1. If ImageName is empty: derive it from Repository, optionally extending
//     with Name. The /<name> suffix is collapsed when Name equals the repo's
//     short name — the resulting <repo>/<repo> would be redundant nesting.
//
//  2. NameSuffix (when non-empty) appends to the repository segment BEFORE
//     the optional /<name>. Used by the dev-release flow.
//
//  3. If the resulting name lacks a "/" or "." (a bare image like "myapp"),
//     prefix it with the registry. docker.io is special: bare names on Docker
//     Hub take the owner as a prefix instead, since "docker.io/myapp" would
//     resolve to the official-images namespace.
func ResolveImageName(in ResolveImageNameInput) string {
	imageName := in.ImageName
	if imageName == "" {
		repo := in.Repository + in.NameSuffix
		repoShort := repoShortName(in.Repository)

		if in.Name != "" && in.Name != repoShort {
			imageName = repo + "/" + in.Name
		} else {
			imageName = repo
		}
	}

	if !strings.Contains(imageName, "/") || !strings.Contains(imageName, ".") {
		if in.Registry == "docker.io" {
			imageName = in.RepositoryOwner + "/" + imageName
		} else {
			imageName = in.Registry + "/" + imageName
		}
	}

	return imageName
}

// repoShortName returns the segment after the last "/" in a "owner/repo" path.
func repoShortName(repository string) string {
	if i := strings.LastIndex(repository, "/"); i >= 0 {
		return repository[i+1:]
	}

	return repository
}
