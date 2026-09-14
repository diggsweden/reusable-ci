// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// writeDist builds a small dist fixture and returns its path.
func writeDist(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")

	for _, f := range []struct{ name, body string }{
		{"app_linux_amd64.tar.gz", "amd64 bytes\n"},
		{"app_linux_arm64.tar.gz", "arm64 bytes\n"},
		{"checksums.txt", "deadbeef  app\n"},
		{"nested/extra.txt", "nested\n"},
	} {
		p := filepath.Join(dist, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(f.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dist
}

func distFixtureDigest(t *testing.T, manifestRoot string) string {
	t.Helper()

	// Fixed, sorted fixture bytes and manifest paths, independent of the product's walk/join/hash code.
	manifest := fmt.Sprintf("%x  %s/app_linux_amd64.tar.gz\n%x  %s/app_linux_arm64.tar.gz\n%x  %s/checksums.txt\n%x  %s/nested/extra.txt\n",
		sha256.Sum256([]byte("amd64 bytes\n")), manifestRoot,
		sha256.Sum256([]byte("arm64 bytes\n")), manifestRoot,
		sha256.Sum256([]byte("deadbeef  app\n")), manifestRoot,
		sha256.Sum256([]byte("nested\n")), manifestRoot)

	return fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
}

func requireShellDigestTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"bash", "find", "sort", "sha256sum", "xargs", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("missing %s; skipping shell cross-check", tool)
		}
	}
}

func shellDigest(t *testing.T, dir string) string {
	t.Helper()

	// The exact dist-digest.sh pipeline, run under the C locale (matches
	// byte-sort). dir is passed as $1 so both Go and the shell hash the
	// same paths.
	script := `find "$1" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'`
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script, "bash", dir) //nolint:gosec // fixed script, test-only.

	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shell pipeline: %v", err)
	}

	return strings.TrimSpace(string(out))
}

func shellDigestFromInside(t *testing.T, dir string) string {
	t.Helper()

	// Nanolinter's image-input hand-off historically digested artifact contents
	// from inside the staging directory so both producer and verifier wrote
	// ./<file> paths even though the outer artifact directory names differed.
	script := `cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'`
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script, "bash", dir) //nolint:gosec // fixed script, test-only.

	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shell pipeline: %v", err)
	}

	return strings.TrimSpace(string(out))
}

// TestDistDigest_MatchesShellPipeline proves byte-compatibility with
// the reusable-workflow digest contract by running the exact pipeline and comparing.
// Skips when the coreutils tools aren't available.
func TestDistDigest_MatchesShellPipeline(t *testing.T) {
	t.Parallel()

	requireShellDigestTools(t)

	dist := writeDist(t)

	got, err := apprelease.DistDigest(dist)
	if err != nil {
		t.Fatal(err)
	}

	want := shellDigest(t, dist)
	if got != want {
		t.Errorf("DistDigest = %s, shell pipeline = %s", got, want)
	}
}

func TestDistDigest_ManifestRootDotMatchesChdirPipeline(t *testing.T) {
	t.Parallel()

	requireShellDigestTools(t)

	dist := writeDist(t)

	got, err := apprelease.DistDigestWithManifestRoot(dist, ".")
	if err != nil {
		t.Fatal(err)
	}

	want := shellDigestFromInside(t, dist)
	if got != want {
		t.Errorf("DistDigestWithManifestRoot = %s, shell pipeline = %s", got, want)
	}
}

func TestDistDigest_DotDirMatchesShellPipeline(t *testing.T) {
	requireShellDigestTools(t)

	dist := writeDist(t)

	t.Chdir(dist)

	got, err := apprelease.DistDigest(".")
	if err != nil {
		t.Fatal(err)
	}

	want := shellDigest(t, ".")
	if got != want {
		t.Errorf("DistDigest = %s, shell pipeline = %s", got, want)
	}
}

func TestVerifyDist_AcceptsAndDetectsMismatch(t *testing.T) {
	t.Parallel()

	dist := writeDist(t)

	digest := distFixtureDigest(t, dist)
	got, err := apprelease.DistDigest(dist)
	require.NoError(t, err)
	require.Equal(t, digest, got)

	if err := apprelease.VerifyDist(dist, digest); err != nil {
		t.Fatalf("clean dist rejected: %v", err)
	}

	if err := apprelease.VerifyDist(dist, "0000"); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("digest mismatch should be a validation error, got %v", err)
	}

	if err := apprelease.VerifyDist(dist, ""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty expected digest should be a usage error, got %v", err)
	}
}

func TestVerifyDist_ExplicitManifestPrefix(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		manifestRoot string
		wantPrefix   string
	}{
		{name: "dot", manifestRoot: ".", wantPrefix: "."},
		{name: "named_prefix", manifestRoot: "handoff/release-v1.2.3///", wantPrefix: "handoff/release-v1.2.3"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			dist := writeDist(t)
			want := distFixtureDigest(t, testCase.wantPrefix)
			got, err := apprelease.DistDigestWithManifestRoot(dist, testCase.manifestRoot)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.NoError(t, apprelease.VerifyDistWithManifestRoot(dist, want, testCase.manifestRoot))
		})
	}
}

func TestVerifyDist_RootBoundaries(t *testing.T) {
	t.Parallel()

	for _, explicit := range []bool{false, true} {
		for _, kind := range []string{"root_symlink", "non_directory", "missing"} {
			t.Run(fmt.Sprintf("explicit_%t/%s", explicit, kind), func(t *testing.T) {
				t.Parallel()
				dist := writeDist(t)

				candidate := filepath.Join(filepath.Dir(dist), kind)
				switch kind {
				case "root_symlink":
					require.NoError(t, os.Symlink(dist, candidate))
				case "non_directory":
					require.NoError(t, os.WriteFile(candidate, []byte("not a directory\n"), 0o600))
				case "missing":
					_, err := os.Lstat(candidate)
					require.ErrorIs(t, err, os.ErrNotExist)
				}

				manifestRoot := candidate
				if explicit {
					manifestRoot = "handoff/release-v1.2.3"
				}

				want := distFixtureDigest(t, manifestRoot)
				// The real root and symlink use the same manifest prefix. Dropping Lstat
				// would therefore accept the link, not fail later on a path-sensitive digest.
				require.NoError(t, apprelease.VerifyDistWithManifestRoot(dist, want, manifestRoot))

				if kind == "root_symlink" {
					got, err := apprelease.DistDigestWithManifestRoot(candidate, manifestRoot)
					require.NoError(t, err)
					require.Equal(t, want, got)
				}

				var err error
				if explicit {
					err = apprelease.VerifyDistWithManifestRoot(candidate, want, manifestRoot)
				} else {
					err = apprelease.VerifyDist(candidate, want)
				}

				switch kind {
				case "root_symlink":
					require.ErrorIs(t, err, errs.ErrValidation)
					require.ErrorContains(t, err, "validate-dist: "+candidate+" must be a real directory, not a symlink")
					target, readErr := os.Readlink(candidate)
					require.NoError(t, readErr)
					require.Equal(t, dist, target)
				case "non_directory":
					require.ErrorIs(t, err, errs.ErrValidation)
					require.ErrorContains(t, err, "validate-dist: "+candidate+" is not a directory")
					body, readErr := os.ReadFile(candidate)
					require.NoError(t, readErr)
					require.Equal(t, "not a directory\n", string(body))
				case "missing":
					require.ErrorIs(t, err, os.ErrNotExist)
					require.ErrorContains(t, err, "validate-dist: stat "+candidate)

					var pathErr *os.PathError
					require.ErrorAs(t, err, &pathErr)
					require.Equal(t, "lstat", pathErr.Op)
					require.Equal(t, candidate, pathErr.Path)
					_, statErr := os.Lstat(candidate)
					require.ErrorIs(t, statErr, os.ErrNotExist)
				}

				require.NoError(t, apprelease.VerifyDistWithManifestRoot(dist, want, manifestRoot))
			})
		}
	}
}

func TestVerifyDist_RejectsSymlink(t *testing.T) {
	t.Parallel()

	dist := writeDist(t)
	digest := distFixtureDigest(t, dist)
	require.NoError(t, apprelease.VerifyDist(dist, digest))

	target := filepath.Join(filepath.Dir(dist), "owned-target")
	require.NoError(t, os.WriteFile(target, []byte("owned target\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dist, "evil")))
	got, err := apprelease.DistDigest(dist)
	require.NoError(t, err)
	require.Equal(t, digest, got)
	err = apprelease.VerifyDist(dist, digest)
	require.ErrorIs(t, err, errs.ErrValidation)
	require.ErrorContains(t, err, "contains a symlink: evil")
}

// TestDistDigest_NamesTheDirectoryInAFailure covers what this layer adds and
// nothing else. The empty-tree rule itself belongs to domainrelease.DistDigest
// and is pinned there; asserting it again here only proved the delegation
// compiles. What is worth pinning at this layer is the wrapping: the caller
// gets a path in the message, because the operator reading it in a CI log has
// several candidate directories and the sentinel alone does not say which one
// was empty.
func TestDistDigest_NamesTheDirectoryInAFailure(t *testing.T) {
	t.Parallel()

	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(dist, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := apprelease.DistDigest(dist)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("empty dist should stay an ErrValidation through the wrapper, got %v", err)
	}

	if !strings.Contains(err.Error(), dist) {
		t.Errorf("error should name the directory that failed; got %q, want it to contain %q", err, dist)
	}
}
