// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package changelog_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/changelog"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestRenderer_GitChglogArgsAndSecretScrub(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git-chglog", `
set -euo pipefail
if [ -n "${SSH_SIGNING_KEY:-}" ] || [ -n "${FORGEJO_TOKEN:-}" ]; then
  echo "secret leaked to git-chglog" >&2
  exit 1
fi
case "$*" in
  '--config full.yml --repository-url https://forgejo.example/owner/repository --next-tag v1.2.3 --output CHANGELOG.md')
    printf '# changelog\n' >CHANGELOG.md
    ;;
  '--config body.yml --repository-url https://forgejo.example/owner/repository --next-tag v1.2.3 v1.2.3')
    printf 'body\n'
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1
    ;;
esac
`)
	t.Setenv("SSH_SIGNING_KEY", "secret")
	t.Setenv("FORGEJO_TOKEN", "secret")

	dir := t.TempDir()
	t.Chdir(dir)

	r := &changelog.Renderer{UnsetEnv: []string{"SSH_SIGNING_KEY", "FORGEJO_TOKEN"}}

	if err := r.RenderFull(context.Background(), "git-chglog", "full.yml", "v1.2.3", "CHANGELOG.md", "https://forgejo.example/owner/repository"); err != nil {
		t.Fatal(err)
	}

	body, err := r.RenderBody(context.Background(), "git-chglog", "body.yml", "v1.2.3", "https://forgejo.example/owner/repository")
	if err != nil {
		t.Fatal(err)
	}

	if body != "body" {
		t.Fatalf("body = %q", body)
	}

	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); err != nil {
		t.Fatal(err)
	}

	if len(m.Invocations("git-chglog")) != 2 {
		t.Fatalf("git-chglog invocations = %d, want 2", len(m.Invocations("git-chglog")))
	}
}

func TestRenderer_GitCliffArgs(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git-cliff", `
set -euo pipefail
case "$*" in
  '--config cliff.toml --unreleased --tag v1.2.3 --output CHANGELOG.md')
    printf '# cliff\n' >CHANGELOG.md
    ;;
  '--config body.toml --unreleased --tag v1.2.3')
    printf 'cliff body\n'
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1
    ;;
esac
`)

	t.Chdir(t.TempDir())

	r := changelog.New()
	if err := r.RenderFull(context.Background(), "git-cliff", "cliff.toml", "v1.2.3", "CHANGELOG.md", "https://forgejo.example/owner/repository"); err != nil {
		t.Fatal(err)
	}

	body, err := r.RenderBody(context.Background(), "git-cliff", "body.toml", "v1.2.3", "https://forgejo.example/owner/repository")
	if err != nil {
		t.Fatal(err)
	}

	if body != "cliff body" {
		t.Fatalf("body = %q", body)
	}

	invocations := m.Invocations("git-cliff")
	if len(invocations) != 2 {
		t.Fatalf("git-cliff invocations = %d, want 2", len(invocations))
	}

	if !slices.Contains(invocations[0].Args, "--unreleased") || !slices.Contains(invocations[0].Args, "--tag") {
		t.Fatalf("git-cliff args = %v", invocations[0].Args)
	}
}

// TestRenderer_BodyIsStdoutAlone covers what git-cliff prints beside the
// body. It writes warnings to stderr while exiting 0, and a combined read
// put " WARN  git_cliff_core::changelog > No releases found" inside a
// release bump commit message. Stderr reaches the caller only on failure.
func TestRenderer_BodyIsStdoutAlone(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git-cliff", `printf ' WARN  git_cliff_core::changelog > No releases found\n' >&2; printf 'feat: body\n'`)

	r := &changelog.Renderer{GitCliffBin: m.Path("git-cliff")}

	body, err := r.RenderBody(context.Background(), "git-cliff", "cliff.toml", "v1.2.3", "")
	if err != nil || body != "feat: body" {
		t.Fatalf("body = %q, %v; want stdout alone", body, err)
	}

	m.Add("git-cliff", `printf 'config error\n' >&2; exit 1`)

	if _, err := r.RenderBody(context.Background(), "git-cliff", "cliff.toml", "v1.2.3", ""); err == nil || !strings.Contains(err.Error(), "config error") {
		t.Fatalf("a failing renderer's stderr did not reach the error: %v", err)
	}
}
