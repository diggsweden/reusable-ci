//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// AddPathspecs and AddPathspecsStrict exist as a pair for one reason:
// the lenient one swallows "pathspec matched nothing" because a version
// bump may legitimately produce nothing to stage, and the strict one
// does not, because a missing changelog must fail the signing step
// rather than become a misleading no-op.
//
// That difference is the whole design and neither half was tested. A
// strict variant that had quietly become lenient would let
// `version changelog-release` commit without the changelog it is named
// for.
//
// No t.Parallel(): isolatedgit.NewRepo scrubs the environment via
// t.Setenv.

func TestAddPathspecs_TheStrictAndLenientHalvesDiffer(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	adapter := &adaptergit.Repo{Dir: repo.Dir}
	ctx := context.Background()

	// An untracked file the caller did not name. Without it the repo is
	// clean, and "nothing was staged" could equally mean the pathspec
	// matched nothing or that the call swept the whole tree into the
	// index and found nothing to sweep.
	if err := os.WriteFile(filepath.Join(repo.Dir, "unrelated.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The lenient half: nothing matched, nothing staged, no error.
	adapter.AddPathspecs(ctx, []string{"CHANGELOG.md"})

	staged, err := adapter.HasStagedChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if staged {
		t.Error("nothing the caller named existed, yet the index changed")
	}

	// The strict half, same input: an error.
	if err := adapter.AddPathspecsStrict(ctx, []string{"CHANGELOG.md"}); err == nil {
		t.Fatal("a pathspec matching nothing was accepted, so a missing changelog " +
			"would become a silent no-op instead of failing the signing step")
	}
}

func TestAddPathspecsStrict_StagesAnExistingFile(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	adapter := &adaptergit.Repo{Dir: repo.Dir}
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(repo.Dir, "CHANGELOG.md"), []byte("# 1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := adapter.AddPathspecsStrict(ctx, []string{"CHANGELOG.md"}); err != nil {
		t.Fatal(err)
	}

	staged, err := adapter.HasStagedChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !staged {
		t.Error("the file was not staged")
	}
}

// TestAddPathspecs_ADashPrefixedPathIsAPathNotAFlag covers the `--`
// separator. Pathspecs reach here from configuration
// (`--file-pattern`), so a value starting with a dash must be staged as
// a filename rather than interpreted as a git option -- `git add -A`
// spelled as a pathspec would stage the entire working tree into a
// release commit.
func TestAddPathspecs_ADashPrefixedPathIsAPathNotAFlag(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	adapter := &adaptergit.Repo{Dir: repo.Dir}
	ctx := context.Background()

	// An unrelated file that a stray `-A` would sweep in.
	if err := os.WriteFile(filepath.Join(repo.Dir, "unrelated.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := adapter.AddPathspecsStrict(ctx, []string{"-A"}); err == nil {
		t.Fatal("`-A` was accepted; if it were read as a flag it would stage the whole tree")
	}

	staged, err := adapter.HasStagedChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if staged {
		t.Error("`-A` was interpreted as a git option and staged the working tree")
	}
}
