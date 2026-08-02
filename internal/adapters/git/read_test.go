// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestReleaseRequestGitHelpersUseHookDisabledRemoteCommands(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git", `
set -euo pipefail
case "$*" in
  '-c core.hooksPath=/dev/null fetch --no-tags origin refs/tags/release-request/v1.2.3:refs/tags/release-request/v1.2.3') ;;
  '-c core.hooksPath=/dev/null ls-remote --tags origin refs/tags/release-request/v1.2.3')
    printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/release-request/v1.2.3\n'
    ;;
  '-c core.hooksPath=/dev/null ls-remote --tags origin refs/tags/v1.2.3') ;;
  *) echo "unexpected git args: $*" >&2; exit 1 ;;
esac
`)

	repo := &adaptergit.Repo{GitBin: m.Path("git")}
	ctx := context.Background()

	if err := repo.FetchTagFromRemote(ctx, "origin", "release-request/v1.2.3"); err != nil {
		t.Fatal(err)
	}

	obj, err := repo.RemoteTagObject(ctx, "origin", "release-request/v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if obj != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("remote object = %q", obj)
	}

	exists, err := repo.RemoteTagExists(ctx, "origin", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Error("final tag should not exist when ls-remote returns no rows")
	}

	invocations := m.Invocations("git")
	if len(invocations) != 3 {
		t.Fatalf("git invocations = %d, want 3", len(invocations))
	}

	for _, inv := range invocations {
		if !slices.Contains(inv.Args, "core.hooksPath=/dev/null") {
			t.Errorf("git args missing hook disable: %s", strings.Join(inv.Args, " "))
		}
	}
}

func TestVerifyTagSSHAgainstAllowedSignersUsesSSHFormat(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git", `
set -euo pipefail
if [ "$*" != '-c core.hooksPath=/dev/null -c gpg.format=ssh -c gpg.ssh.allowedSignersFile=allowed_signers tag -v release-request/v1.2.3' ]; then
  echo "unexpected git args: $*" >&2
  exit 1
fi
printf 'Good "git" signature for release-request/v1.2.3\n'
`)

	repo := &adaptergit.Repo{GitBin: m.Path("git")}

	ok, out, err := repo.VerifyTagSSHAgainstAllowedSigners(context.Background(), "release-request/v1.2.3", "allowed_signers")
	if err != nil {
		t.Fatal(err)
	}

	if !ok || !strings.Contains(out, "Good") {
		t.Fatalf("VerifyTagSSHAgainstAllowedSigners = (%v, %q), want ok output", ok, out)
	}
}
