// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
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

// TestBuild_ModeDispatch covers Build's mode switch, which was uncovered.
// Each mode contributes a different tail to the argv, and two modes are
// refusals rather than builds.
func TestBuild_ModeDispatch(t *testing.T) {
	// No t.Parallel(): mockbinary prepends to PATH via t.Setenv.
	for _, tc := range []struct {
		name     string
		req      container.BuildRequest
		wantTail []string
		wantErr  bool
	}{
		{
			// load: tag the image into local storage.
			name:     "load tags the image",
			req:      container.BuildRequest{Context: ".", Mode: container.BuildModeLoad, ImageRef: "localhost/app:arch"},
			wantTail: []string{"-t", "localhost/app:arch", "."},
		},
		{
			// local: write the filesystem out instead of an image.
			name:     "local writes to a directory",
			req:      container.BuildRequest{Context: ".", Mode: container.BuildModeLocal, OutputDir: "dist"},
			wantTail: []string{"--output", "type=local,dest=dist", "."},
		},
		{
			// Refused deliberately: a push-by-digest build has to go
			// through BuildToLayout so the digest comes from the layout
			// rather than from a tag that could move.
			name:    "push-by-digest is redirected",
			req:     container.BuildRequest{Context: ".", Mode: container.BuildModePushByDigest, ImageRef: "registry.example/app"},
			wantErr: true,
		},
		{
			name:    "unknown mode",
			req:     container.BuildRequest{Context: ".", Mode: container.BuildOutputMode("sideways"), ImageRef: "x"},
			wantErr: true,
		},
		{
			// Validation runs before the switch, so an unusable request
			// never reaches buildah.
			name:    "no build context",
			req:     container.BuildRequest{Mode: container.BuildModeLoad, ImageRef: "x"},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
			m.Add("buildah", `printf 'ARGV %s\n' "$*" >&2`)

			a := &Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			err := a.Build(context.Background(), tc.req, &stderr)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr {
				if !errors.Is(err, errs.ErrUsage) {
					t.Errorf("err = %v, want ErrUsage", err)
				}

				if stderr.Len() != 0 {
					t.Errorf("buildah was invoked on a refused request: %s", stderr.String())
				}

				return
			}

			got := strings.Fields(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "ARGV "))
			if len(got) < len(tc.wantTail) {
				t.Fatalf("argv = %q, too short for tail %q", got, tc.wantTail)
			}

			if tail := got[len(got)-len(tc.wantTail):]; !slices.Equal(tail, tc.wantTail) {
				t.Errorf("argv tail = %q, want %q (full: %q)", tail, tc.wantTail, got)
			}
		})
	}
}
