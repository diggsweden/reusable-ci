// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

func TestResolveImageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   container.ResolveImageNameInput
		want string
	}{
		// === GHCR ===
		{
			name: "ghcr.io: explicit bare name gets registry prefix",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "myapp", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Repository: "owner/repo", RepositoryOwner: "owner", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
			want: "ghcr.io/myapp",
		},
		{
			name: "ghcr.io: empty image name uses repository",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "",
				Repository: "myorg/myrepo", RepositoryOwner: "myorg", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
			want: "ghcr.io/myorg/myrepo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		{
			name: "ghcr.io: explicit bare name overrides repository",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "backend",
				Repository: "diggsweden/workflow", RepositoryOwner: "diggsweden",
			},
			want: "ghcr.io/backend",
		},

		// === Docker Hub ===
		{
			name: "docker.io: bare name takes owner prefix",
			in: container.ResolveImageNameInput{
				Registry: "docker.io", ImageName: "myapp", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Repository: "owner/repo", RepositoryOwner: "owner",
			},
			want: "owner/myapp",
		},
		{
			// Bash quirk preserved: empty image name on docker.io with a
			// "owner/project" repository ends up double-prefixed because
			// "owner/project" lacks a "." (only / triggers the prefix branch).
			name: "docker.io: empty image name double-prefixes owner",
			in: container.ResolveImageNameInput{
				Registry: "docker.io", ImageName: "",
				Repository: "username/project", RepositoryOwner: "username",
			},
			want: "username/username/project",
		},

		// === Multi-container Name handling ===
		{
			name: "Name = repo short → collapses to repository (no double-nesting)",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "",
				Repository: "owner/repo", RepositoryOwner: "owner",
				Name: "repo",
			},
			want: "ghcr.io/owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		{
			name: "Name distinct from repo → repository/name suffix",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "",
				Repository: "owner/repo", RepositoryOwner: "owner",
				Name: "frontend",
			},
			want: "ghcr.io/owner/repo/frontend",
		},

		// === Already-fully-qualified passes through ===
		{
			name: "already-qualified ghcr.io passes through",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "ghcr.io/owner/repo",
				Repository: "owner/repo", RepositoryOwner: "owner",
			},
			want: "ghcr.io/owner/repo",
		},
		{
			name: "already-qualified docker.io passes through",
			in: container.ResolveImageNameInput{
				Registry: "docker.io", ImageName: "docker.io/library/nginx",
				Repository: "owner/repo", RepositoryOwner: "owner",
			},
			want: "docker.io/library/nginx",
		},

		// === Case normalisation (OCI names must be lowercase) ===
		{
			name: "mixed-case repo/owner is lowercased",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "",
				Repository: "OctoCat/Hello-World", RepositoryOwner: "OctoCat",
			},
			want: "ghcr.io/octocat/hello-world",
		},
		{
			name: "explicit mixed-case image-name is lowercased too",
			in: container.ResolveImageNameInput{
				Registry: "ghcr.io", ImageName: "ghcr.io/Org/App",
				Repository: "Org/App", RepositoryOwner: "Org",
			},
			want: "ghcr.io/org/app",
		},

		// === Custom registry (registry.gitlab.com) ===
		{
			name: "custom registry: bare name gets prefix",
			in: container.ResolveImageNameInput{
				Registry: "registry.gitlab.com", ImageName: "myapp", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Repository: "group/project", RepositoryOwner: "group", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
			want: "registry.gitlab.com/myapp",
		},
		{
			name: "custom registry: empty image name uses repository",
			in: container.ResolveImageNameInput{
				Registry: "registry.gitlab.com", ImageName: "",
				Repository: "group/sub/project", RepositoryOwner: "group",
			},
			want: "registry.gitlab.com/group/sub/project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := container.ResolveImageName(tc.in)
			if got != tc.want {
				t.Errorf("ResolveImageName(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
