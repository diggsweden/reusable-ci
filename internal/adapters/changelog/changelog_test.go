// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package changelog_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
  '--config full.yml --next-tag v1.2.3 --output CHANGELOG.md')
    printf '# changelog\n' >CHANGELOG.md
    ;;
  '--config body.yml --next-tag v1.2.3 v1.2.3')
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

	r := changelog.New()

	if err := r.RenderFull(context.Background(), "git-chglog", "full.yml", "v1.2.3", "CHANGELOG.md"); err != nil {
		t.Fatal(err)
	}

	body, err := r.RenderBody(context.Background(), "git-chglog", "body.yml", "v1.2.3")
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
	if err := r.RenderFull(context.Background(), "git-cliff", "cliff.toml", "v1.2.3", "CHANGELOG.md"); err != nil {
		t.Fatal(err)
	}

	body, err := r.RenderBody(context.Background(), "git-cliff", "body.toml", "v1.2.3")
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
