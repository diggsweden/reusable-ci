// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah

import (
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

const (
	layers    = "--layers"
	cacheFrom = "--cache-from"
	cacheRef  = "ghcr.io/org/cache:main"
)

// buildArgs is pure argv construction, and the only tests that reached it
// were the integration ones behind a build tag — so the ordinary unit run
// covered none of it. What it assembles is worth holding down: the secret
// mounts, the OCI labels, and the reproducibility timestamp.
func TestBuildArgs_Composition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a    *Adapter
		req  container.BuildRequest
		mode []string
		want []string
	}{
		{
			name: "context only",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: "."},
			want: []string{"build", "."},
		},
		{
			// Global flags come first and the context always last, with
			// the mode arguments immediately before it.
			name: "global flags lead, context trails",
			a:    &Adapter{Global: []string{"--storage-driver", "vfs"}},
			req:  container.BuildRequest{Context: "src"},
			mode: []string{"--tag", "img:latest"},
			want: []string{"--storage-driver", "vfs", "build", "--tag", "img:latest", "src"},
		},
		{
			name: "every optional flag",
			a:    &Adapter{},
			req: container.BuildRequest{
				Context:       "ctx",
				Containerfile: "Containerfile.alt",
				Platform:      "linux/arm64",
				Target:        "runtime",
				BuildArgs:     []string{"VERSION=1.2.3", "COMMIT=abc"},
				Labels:        []string{"org.opencontainers.image.revision=abc"},
			},
			want: []string{
				"build",
				"-f", "Containerfile.alt",
				"--platform", "linux/arm64",
				"--target", "runtime",
				"--build-arg", "VERSION=1.2.3",
				"--build-arg", "COMMIT=abc",
				"--label", "org.opencontainers.image.revision=abc",
				"ctx",
			},
		},
		{
			// Each secret becomes its own --secret, passed through
			// verbatim: the id=NAME,src=PATH text is the contract with
			// `container materialize-build-secrets`.
			name: "secrets are one flag each, verbatim",
			a:    &Adapter{},
			req: container.BuildRequest{
				Context: ".",
				Secrets: []string{"id=db_password,src=/tmp/build-secrets/db_password", "id=api_token,src=/tmp/build-secrets/api_token"},
			},
			want: []string{
				"build",
				"--secret", "id=db_password,src=/tmp/build-secrets/db_password",
				"--secret", "id=api_token,src=/tmp/build-secrets/api_token",
				".",
			},
		},
		{
			// --timestamp, not --source-date-epoch: the comment on this
			// branch explains the choice (older buildah lacks the latter),
			// and it is what makes a rebuild of the same source produce
			// the same digest.
			name: "source date epoch pins the timestamp",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: ".", SourceDateEpoch: "1700000000"},
			want: []string{"build", "--timestamp", "1700000000", "."},
		},
		{
			name: "no source date epoch leaves the timestamp alone",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: "."},
			want: []string{"build", "."},
		},
		{
			// Import is best-effort and always safe; exporting is not, so
			// --cache-to appears only when the caller opted in.
			name: "cache import only",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: ".", CacheRepo: "ghcr.io/org/cache", CacheScope: "main"},
			want: []string{"build", layers, cacheFrom, cacheRef, "."},
		},
		{
			name: "cache import and export",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: ".", CacheRepo: "ghcr.io/org/cache", CacheScope: "main", CachePush: true},
			want: []string{"build", layers, cacheFrom, cacheRef, "--cache-to", cacheRef, "."},
		},
		{
			// No cache repo disables caching entirely, CachePush or not.
			name: "cache push without a repo does nothing",
			a:    &Adapter{},
			req:  container.BuildRequest{Context: ".", CachePush: true},
			want: []string{"build", "."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.a.buildArgs(tc.req, tc.mode...); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildArgs =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestGlobal_DoesNotAliasTheSharedSlice covers what the helper's comment
// promises. Global is shared across every invocation on the adapter, so a
// buildArgs call that appended into its backing array would leak the
// previous build's flags into the next one.
func TestGlobal_DoesNotAliasTheSharedSlice(t *testing.T) {
	t.Parallel()

	// Spare capacity, so a careless append writes into the shared array
	// rather than allocating.
	shared := make([]string, 1, 8)
	shared[0] = "--storage-driver"

	a := &Adapter{Global: append(shared, "vfs")}

	first := a.buildArgs(container.BuildRequest{Context: "one", Target: "a"})
	second := a.buildArgs(container.BuildRequest{Context: "two"})

	if !reflect.DeepEqual(first, []string{"--storage-driver", "vfs", "build", "--target", "a", "one"}) {
		t.Errorf("first = %q", first)
	}

	if !reflect.DeepEqual(second, []string{"--storage-driver", "vfs", "build", "two"}) {
		t.Errorf("second call saw the first call's flags: %q", second)
	}

	if got := len(a.Global); got != 2 {
		t.Errorf("Global grew to %d entries: %q", got, a.Global)
	}
}
