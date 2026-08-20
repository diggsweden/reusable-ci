// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package skopeo_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/skopeo"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestAdapter_CopyDockerToOCIArchive_ArgvShape pins the copy invocation.
// --retry-times is what makes a transient registry error a retry rather
// than a failed release, and the transport prefixes are what decide
// whether skopeo reads a registry or a file.
func TestAdapter_CopyDockerToOCIArchive_ArgvShape(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("skopeo", `printf '%s\n' "$*" >&2`)

	var stderr bytes.Buffer

	a := &skopeo.Adapter{Bin: m.Path("skopeo")}
	if err := a.CopyDockerToOCIArchive(context.Background(), "ghcr.io/org/app@sha256:abc", "out.tar", &stderr); err != nil {
		t.Fatal(err)
	}

	want := "copy --retry-times 5 docker://ghcr.io/org/app@sha256:abc oci-archive:out.tar"
	if got := strings.TrimSpace(stderr.String()); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestAdapter_CopyDockerToOCIArchive_AuthFile(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `printf '%s\n' "$*" >&2`)

	var stderr bytes.Buffer

	a := skopeo.WithAuthFile("/tmp/auth.json")
	a.Bin = m.Path("skopeo")

	if err := a.CopyDockerToOCIArchive(context.Background(), "ghcr.io/org/app:1", "out.tar", &stderr); err != nil {
		t.Fatal(err)
	}

	// Before the transports, so it applies to the source pull.
	want := "copy --retry-times 5 --authfile /tmp/auth.json docker://ghcr.io/org/app:1 oci-archive:out.tar"
	if got := strings.TrimSpace(stderr.String()); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestAdapter_CopyDockerToOCIArchive_RejectsEmptyRefOrArchive(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `printf 'skopeo ran\n' >&2; exit 1`)

	a := &skopeo.Adapter{Bin: m.Path("skopeo")}

	for _, tc := range []struct{ name, ref, archive string }{
		{name: "no ref", ref: "", archive: "out.tar"},
		{name: "no archive", ref: "ghcr.io/org/app:1", archive: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer

			if err := a.CopyDockerToOCIArchive(context.Background(), tc.ref, tc.archive, &stderr); err == nil {
				t.Fatal("expected an error")
			}

			// Refused before skopeo is spawned.
			if stderr.Len() != 0 {
				t.Errorf("skopeo was invoked anyway: %q", stderr.String())
			}
		})
	}
}

// TestAdapter_CanUnsetSensitiveEnv is the skopeo counterpart of the syft
// adapter's scrub test. The field and the envWithout helper behind it are
// byte-identical between the two packages, and only syft's copy was
// covered.
//
// Note that no caller sets UnsetEnv on this adapter today, so the
// scrubbing is machinery that is built but not wired -- in the same
// command where syft is given signerSecretEnv(). See
// docs/open-questions.md.
func TestAdapter_CanUnsetSensitiveEnv(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `
if [ -n "${COSIGN_KEY:-}" ] || [ -n "${REGISTRY_TOKEN:-}" ]; then
  printf 'secret env leaked to skopeo\n' >&2
  exit 42
fi
if [ -z "${PATH:-}" ]; then
  printf 'unrelated env was dropped too\n' >&2
  exit 43
fi
printf 'ok\n' >&2
`)
	t.Setenv("COSIGN_KEY", "secret")
	t.Setenv("REGISTRY_TOKEN", "token")

	var stderr bytes.Buffer

	a := &skopeo.Adapter{Bin: m.Path("skopeo"), UnsetEnv: []string{"COSIGN_KEY", "REGISTRY_TOKEN"}}
	if err := a.CopyDockerToOCIArchive(context.Background(), "ghcr.io/org/app:1", "out.tar", &stderr); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}

	if got := strings.TrimSpace(stderr.String()); got != "ok" {
		t.Errorf("stderr = %q", got)
	}
}

// The two OCI-layout copy variants had no test. Both feed the scanner
// that produces an SBOM, so copying the wrong platform or the wrong
// image produces an SBOM that describes something the release does not
// ship.
//
// No t.Parallel(): mockbinary prepends to PATH via t.Setenv.

func TestAdapter_CopyDockerDigestToOCILayout_ArgvShape(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("skopeo", `printf '%s\n' "$*" >&2`)

	var stderr bytes.Buffer

	a := &skopeo.Adapter{Bin: m.Path("skopeo"), AuthFile: "/run/auth.json"}
	if err := a.CopyDockerDigestToOCILayout(context.Background(),
		"ghcr.io/org/app", "sha256:abc", "/tmp/layout", "linux", "arm64", &stderr); err != nil {
		t.Fatal(err)
	}

	// The source is pinned by digest, not by the tag the caller passed:
	// a tag can move between the copy and the scan.
	want := "copy --retry-times 5 --override-arch arm64 --override-os linux " +
		"--authfile /run/auth.json docker://ghcr.io/org/app@sha256:abc oci:/tmp/layout:scan"
	if got := strings.TrimSpace(stderr.String()); got != want {
		t.Errorf("argv = %q,\nwant      %q", got, want)
	}
}

// TestAdapter_CopyOCILayoutToOCILayout_PassesNoCredential is the claim
// worth having twice: a layout-to-layout copy never touches a registry,
// so it is not given the auth file even when the adapter holds one.
func TestAdapter_CopyOCILayoutToOCILayout_PassesNoCredential(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `printf '%s\n' "$*" >&2`)

	var stderr bytes.Buffer

	a := &skopeo.Adapter{Bin: m.Path("skopeo"), AuthFile: "/run/auth.json"}
	if err := a.CopyOCILayoutToOCILayout(context.Background(),
		"/tmp/src", "/tmp/dst", "linux", "amd64", &stderr); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(stderr.String())
	if strings.Contains(got, "--authfile") || strings.Contains(got, "auth.json") {
		t.Errorf("a local layout-to-layout copy was handed a registry credential: %q", got)
	}

	// No retries either: there is no network to be flaky, so a retry
	// loop would only slow a genuine failure down.
	want := "copy --override-arch amd64 --override-os linux oci:/tmp/src:scan oci:/tmp/dst:scan"
	if got != want {
		t.Errorf("argv = %q,\nwant      %q", got, want)
	}
}

// TestAdapter_CopyToOCILayout_RequiresEveryInput covers the refusals. An
// empty platform is the one that matters most: skopeo without
// --override-arch copies the host's platform, so an empty arch would
// silently scan whatever the runner happens to be.
func TestAdapter_CopyToOCILayout_RequiresEveryInput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("skopeo", `printf '%s\n' "$*" >&2`)

	a := &skopeo.Adapter{Bin: m.Path("skopeo")}
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"digest copy without a ref", func() error {
			return a.CopyDockerDigestToOCILayout(ctx, "", "sha256:abc", "/dst", "linux", "amd64", nil)
		}},
		{"digest copy without a digest", func() error {
			return a.CopyDockerDigestToOCILayout(ctx, "ghcr.io/org/app", "", "/dst", "linux", "amd64", nil)
		}},
		{"digest copy without a destination", func() error {
			return a.CopyDockerDigestToOCILayout(ctx, "ghcr.io/org/app", "sha256:abc", "", "linux", "amd64", nil)
		}},
		{"digest copy without an os", func() error {
			return a.CopyDockerDigestToOCILayout(ctx, "ghcr.io/org/app", "sha256:abc", "/dst", "", "amd64", nil)
		}},
		{"digest copy without an arch", func() error {
			return a.CopyDockerDigestToOCILayout(ctx, "ghcr.io/org/app", "sha256:abc", "/dst", "linux", "", nil)
		}},
		{"layout copy without a source", func() error {
			return a.CopyOCILayoutToOCILayout(ctx, "", "/dst", "linux", "amd64", nil)
		}},
		{"layout copy without a destination", func() error {
			return a.CopyOCILayoutToOCILayout(ctx, "/src", "", "linux", "amd64", nil)
		}},
		{"layout copy without an arch", func() error {
			return a.CopyOCILayoutToOCILayout(ctx, "/src", "/dst", "linux", "", nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Error("accepted, so skopeo would run with an incomplete reference")
			}
		})
	}

	// Nothing above may have reached the binary: a refusal that still
	// spawned skopeo would copy something.
	if got := len(m.Invocations("skopeo")); got != 0 {
		t.Errorf("skopeo was invoked %d times despite every call being refused", got)
	}
}
