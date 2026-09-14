// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaggerBoundary_EmptyLightweightMetadata(t *testing.T) {
	testenv.New(t)
	dir := t.TempDir()
	git, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	sig := object.Signature{Name: "Fixture", Email: "fixture@example.test", When: time.Unix(1700000000, 0).UTC()}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "fixture"}
	encoded := git.Storer.NewEncodedObject()
	require.NoError(t, commit.Encode(encoded))
	hash, err := git.Storer.SetEncodedObject(encoded)
	require.NoError(t, err)
	require.NoError(t, git.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName("light"), hash)))
	bin := filepath.Join(t.TempDir(), "git-fixture")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\ncase \"$*\" in *taggername*) printf ' <>\\n';; *taggerdate*) printf '\\n';; *) exit 1;; esac\n"), 0o700)) //nolint:gosec // owned fixed-output fixture, not Git.

	for _, repo := range []*adaptergit.Repo{{Dir: dir, GitBin: bin}, {Dir: t.TempDir(), GitBin: bin}} {
		info, tagErr := repo.TaggerInfo(t.Context(), "light")
		require.NoError(t, tagErr)
		require.Equal(t, domaingit.TaggerInfo{}, info)
	}

	tag := &object.Tag{Name: "annotated", Tagger: sig, Target: hash, TargetType: plumbing.CommitObject, Message: "release"}
	encoded = git.Storer.NewEncodedObject()
	require.NoError(t, tag.Encode(encoded))
	tagHash, err := git.Storer.SetEncodedObject(encoded)
	require.NoError(t, err)
	require.NoError(t, git.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName("annotated"), tagHash)))
	info, err := (&adaptergit.Repo{Dir: dir, GitBin: bin}).TaggerInfo(t.Context(), "annotated")
	require.NoError(t, err)
	require.Equal(t, "Fixture <fixture@example.test>", info.Tagger)
	require.Equal(t, "2023-11-14 22:13:20 +0000", info.Date)
}
