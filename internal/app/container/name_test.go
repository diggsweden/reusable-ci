// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"slices"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// TestResolveName_WritesNameOutput checks the use case hands every input to
// the resolver and publishes exactly one output. The naming rules themselves
// are tabled at the domain boundary; each row here depends on a different
// field reaching it: the registry, the owner (Docker Hub's prefix), the
// sub-name, the explicit override, and the repository's case.
func TestResolveName_WritesNameOutput(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in   appcontainer.ResolveNameInput
		want string
	}{
		"repository under the registry": {
			in:   appcontainer.ResolveNameInput{Registry: "ghcr.io", Repository: "owner/repo", RepositoryOwner: "owner"},
			want: "ghcr.io/owner/repo",
		},
		"sub-name appended": {
			in:   appcontainer.ResolveNameInput{Registry: "registry.gitlab.com", Repository: "group/project", RepositoryOwner: "group", Name: "frontend"},
			want: "registry.gitlab.com/group/project/frontend",
		},
		"docker hub bare name takes the owner": {
			in:   appcontainer.ResolveNameInput{Registry: "docker.io", ImageName: "app", Repository: "owner/repo", RepositoryOwner: "hubuser"},
			want: "hubuser/app",
		},
		"mixed-case repository is lowercased": {
			in:   appcontainer.ResolveNameInput{Registry: "ghcr.io", Repository: "OctoCat/Hello-World", RepositoryOwner: "OctoCat"},
			want: "ghcr.io/octocat/hello-world",
		},
		"qualified override replaces the registry": {
			in:   appcontainer.ResolveNameInput{Registry: "ghcr.io", ImageName: "localhost:5000/team/app", Repository: "owner/repo", RepositoryOwner: "owner"},
			want: "localhost:5000/team/app",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := fakeoutputsink.New(t)

			if err := appcontainer.ResolveName(context.Background(), sink, tc.in); err != nil {
				t.Fatalf("ResolveName: %v", err)
			}

			if keys := sink.Keys(); !slices.Equal(keys, []string{"name"}) {
				t.Errorf("outputs = %v, want only name", keys)
			}

			if got := sink.Single("name"); got != tc.want {
				t.Errorf("name = %q, want %q", got, tc.want)
			}
		})
	}
}
