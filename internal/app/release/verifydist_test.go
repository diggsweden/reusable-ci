// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

// TestDistDigest_MatchesShellPipeline proves byte-compatibility with
// forgejo-ci's dist-digest.sh by running the exact pipeline and comparing.
// Skips when the coreutils tools aren't available.
func TestDistDigest_MatchesShellPipeline(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "find", "sort", "sha256sum", "xargs", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("missing %s; skipping shell cross-check", tool)
		}
	}

	dist := writeDist(t)

	got, err := apprelease.DistDigest(dist)
	if err != nil {
		t.Fatal(err)
	}

	// The exact dist-digest.sh pipeline, run under the C locale (matches
	// byte-sort). dist is passed as $1 so both Go and the shell hash the
	// same absolute paths.
	script := `find "$1" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'`
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script, "bash", dist) //nolint:gosec // fixed script, test-only.

	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shell pipeline: %v", err)
	}

	want := strings.TrimSpace(string(out))
	if got != want {
		t.Errorf("DistDigest = %s, shell pipeline = %s", got, want)
	}
}

func TestVerifyDist_AcceptsAndDetectsMismatch(t *testing.T) {
	t.Parallel()

	dist := writeDist(t)

	digest, err := apprelease.DistDigest(dist)
	if err != nil {
		t.Fatal(err)
	}

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

func TestVerifyDist_RejectsSymlink(t *testing.T) {
	t.Parallel()

	dist := writeDist(t)

	if err := os.Symlink("/etc/hostname", filepath.Join(dist, "evil")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	digest, _ := apprelease.DistDigest(dist)
	if err := apprelease.VerifyDist(dist, digest); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("a symlink in dist must be rejected, got %v", err)
	}
}
