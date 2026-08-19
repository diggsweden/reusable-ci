// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// pushStub is a buildah stand-in that writes a digest to whatever
// --digestfile it was given and reports its argv and the auth-related
// environment it was handed.
const pushStub = `
digest_path=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--digestfile" ]; then digest_path="$a"; fi
  prev="$a"
done
[ -n "$digest_path" ] && printf 'sha256:feedface\n' > "$digest_path"
printf 'ARGV %s\n' "$*" >&2
printf 'DIGESTFILE %s\n' "$digest_path" >&2
printf 'REGISTRY_AUTH_FILE=%s\n' "${REGISTRY_AUTH_FILE:-<unset>}" >&2
`

// TestPushImageToRefWithDigest covers a function with no coverage at all,
// including under the integration tag. It is what actually publishes a
// release image.
func TestPushImageToRefWithDigest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authFile  string
		tlsVerify bool
		wantTLS   string
		wantAuth  bool
	}{
		{name: "tls on with an auth file", authFile: "/tmp/auth.json", tlsVerify: true, wantTLS: "--tls-verify=true", wantAuth: true},
		{name: "tls off", authFile: "/tmp/auth.json", tlsVerify: false, wantTLS: "--tls-verify=false", wantAuth: true},
		{name: "no auth file", tlsVerify: true, wantTLS: "--tls-verify=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
			m.Add("buildah", pushStub)

			a := &buildah.Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			digest, err := a.PushImageToRefWithDigest(context.Background(), tc.authFile, tc.tlsVerify, "localhost/app:arch", "registry.example/app:v1", &stderr)
			if err != nil {
				t.Fatal(err)
			}

			// The digest comes back from the file buildah wrote, not from
			// anything this code guessed.
			if strings.TrimSpace(digest) != "sha256:feedface" {
				t.Errorf("digest = %q, want the value buildah wrote to --digestfile", digest)
			}

			log := stderr.String()

			// The TLS flag is always explicit, never left to buildah's
			// default, so a push cannot silently skip verification.
			if !strings.Contains(log, tc.wantTLS) {
				t.Errorf("argv missing %q:\n%s", tc.wantTLS, log)
			}

			// The destination carries the docker:// transport, or buildah
			// would write to local storage instead of the registry.
			if !strings.Contains(log, "docker://registry.example/app:v1") {
				t.Errorf("argv missing the docker:// destination:\n%s", log)
			}

			assertAuthPlumbing(t, log, tc.authFile, tc.wantAuth)
			assertDigestFileRemoved(t, log)
		})
	}
}

// TestPushManifestToRefWithDigest covers the manifest-list push, which a
// multi-arch release actually uses.
func TestPushManifestToRefWithDigest(t *testing.T) {
	for _, remove := range []bool{true, false} {
		name := "keeps the local manifest"
		if remove {
			name = "removes the local manifest"
		}

		t.Run(name, func(t *testing.T) {
			m := mockbinary.New(t)
			m.Add("buildah", pushStub)

			a := &buildah.Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			digest, err := a.PushManifestToRefWithDigest(context.Background(), "/tmp/auth.json", true, "localhost/app:list", "registry.example/app:v1", remove, &stderr)
			if err != nil {
				t.Fatal(err)
			}

			if strings.TrimSpace(digest) != "sha256:feedface" {
				t.Errorf("digest = %q", digest)
			}

			log := stderr.String()

			// --all is what pushes every architecture in the list; without
			// it a multi-arch release ships one arch.
			if !strings.Contains(log, "--all") {
				t.Errorf("argv missing --all:\n%s", log)
			}

			if got := strings.Contains(log, "--rm"); got != remove {
				t.Errorf("--rm present = %v, want %v:\n%s", got, remove, log)
			}

			assertDigestFileRemoved(t, log)
		})
	}
}

// assertAuthPlumbing checks that the auth file reaches buildah twice on
// purpose -- as a flag and as the environment variable its child tooling
// reads -- and that neither appears when no auth file was given.
func assertAuthPlumbing(t *testing.T, log, authFile string, want bool) {
	t.Helper()

	if !want {
		if strings.Contains(log, "--authfile") {
			t.Errorf("argv carries --authfile with no auth file set:\n%s", log)
		}

		if !strings.Contains(log, "REGISTRY_AUTH_FILE=<unset>") {
			t.Errorf("REGISTRY_AUTH_FILE set with no auth file:\n%s", log)
		}

		return
	}

	if !strings.Contains(log, "--authfile "+authFile) {
		t.Errorf("argv missing --authfile:\n%s", log)
	}

	if !strings.Contains(log, "REGISTRY_AUTH_FILE="+authFile) {
		t.Errorf("REGISTRY_AUTH_FILE not passed to the subprocess:\n%s", log)
	}
}

// assertDigestFileRemoved checks that the temp file buildah wrote the
// digest into is gone once the call returns. It lives in the shared
// system temp directory, so a leak accumulates there for the life of the
// host.
//
// The path is taken from what the stub reported rather than by scanning
// the temp directory, which would make the assertion depend on whatever
// else happens to be in /tmp.
func assertDigestFileRemoved(t *testing.T, log string) {
	t.Helper()

	const marker = "DIGESTFILE "

	i := strings.Index(log, marker)
	if i < 0 {
		t.Fatalf("stub did not report a digest file:\n%s", log)
	}

	path, _, _ := strings.Cut(log[i+len(marker):], "\n")
	if strings.TrimSpace(path) == "" {
		t.Fatal("buildah was given no --digestfile")
	}

	if _, err := os.Stat(strings.TrimSpace(path)); !os.IsNotExist(err) {
		t.Errorf("digest file %q survived the call (stat err = %v)", path, err)
	}
}

// TestBuildManifest_ArgvShape covers the multi-arch build invocation,
// which had no coverage under either tag. Its argv carries four
// decisions worth holding in place.
func TestBuildManifest_ArgvShape(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("buildah", `
printf 'ARGV %s\n' "$*" >&2
printf 'REGISTRY_AUTH_FILE=%s\n' "${REGISTRY_AUTH_FILE:-<unset>}" >&2
`)

	a := &buildah.Adapter{Bin: m.Path("buildah")}

	var stderr strings.Builder

	err := a.BuildManifest(context.Background(), container.BuildPushManifestBuildRequest{
		AuthFile:        "/tmp/auth.json",
		Manifest:        "localhost/app:list",
		TLSVerify:       true,
		SourceDateEpoch: "1700000000",
		Platform:        "linux/arm64",
		Containerfile:   "Containerfile",
		BuildArgs:       []string{"VERSION=1.2.3", "COMMIT=abc"},
		Labels:          []string{"org.opencontainers.image.revision=abc"},
		Context:         ".",
	}, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	log := stderr.String()

	for _, want := range []struct{ flag, why string }{
		// A stale or poisoned base image in local storage must never be
		// used for a release build.
		{flag: "--pull=always", why: "base images are always re-pulled"},
		// Build isolation.
		{flag: "--isolation chroot", why: "builds run isolated"},
		// Explicit rather than left to buildah's default.
		{flag: "--tls-verify=true", why: "TLS verification is stated"},
		// The reproducibility pin: same source, same digest.
		{flag: "--timestamp 1700000000", why: "the build is reproducible"},
		// Without this the per-arch image is not added to the list.
		{flag: "--manifest localhost/app:list", why: "the image joins the manifest list"},
		{flag: "--platform linux/arm64", why: "the target arch is set"},
		{flag: "--label org.opencontainers.image.revision=abc", why: "labels reach the image"},
		{flag: "--build-arg VERSION=1.2.3", why: "build args reach the build"},
	} {
		if !strings.Contains(log, want.flag) {
			t.Errorf("argv missing %q (%s):\n%s", want.flag, want.why, log)
		}
	}

	// Unlike the push calls, the build passes the auth file only through
	// the environment -- buildah bud reads REGISTRY_AUTH_FILE to
	// authenticate the base-image pull, and no --authfile flag is added.
	if !strings.Contains(log, "REGISTRY_AUTH_FILE=/tmp/auth.json") {
		t.Errorf("REGISTRY_AUTH_FILE not passed to the build:\n%s", log)
	}

	if strings.Contains(log, "--authfile") {
		t.Errorf("build unexpectedly carries --authfile:\n%s", log)
	}
}

// TestExportLocalToLayout_ArgvShape covers the two OCI-layout exports the
// evidence pipeline scans from. Both were uncovered.
func TestExportLocalToLayout_ArgvShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(a *buildah.Adapter, w *strings.Builder) error
		want string
	}{
		{
			name: "single image",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.ExportLocalImageToLayout(context.Background(), "localhost/app:arch", "/tmp/layout", w)
			},
			want: "push localhost/app:arch oci:/tmp/layout:scan",
		},
		{
			// --all again: the evidence scan must see every architecture,
			// not just whichever one the list happens to resolve to.
			name: "manifest list",
			call: func(a *buildah.Adapter, w *strings.Builder) error {
				return a.ExportLocalManifestToLayout(context.Background(), "localhost/app:list", "/tmp/layout", w)
			},
			want: "manifest push --all localhost/app:list oci:/tmp/layout:scan",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t)
			m.Add("buildah", `printf 'ARGV %s\n' "$*" >&2`)

			a := &buildah.Adapter{Bin: m.Path("buildah")}

			var stderr strings.Builder

			if err := tc.call(a, &stderr); err != nil {
				t.Fatal(err)
			}

			if got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "ARGV")); got != tc.want {
				t.Errorf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}
