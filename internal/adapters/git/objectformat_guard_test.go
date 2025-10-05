// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitObjectFormat_ExplicitSHA1IgnoresDefaults(t *testing.T) {
	testenv.New(t)
	root := t.TempDir()
	record := filepath.Join(root, "args")
	binary := filepath.Join(root, "git-fixture")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RECORDED_GIT_ARGS\"\n"), 0o700)) //nolint:gosec // owned executable fixture, never a real Git binary.
	t.Setenv("RECORDED_GIT_ARGS", record)
	t.Setenv("GIT_DEFAULT_HASH", "sha256")

	repo := &git.Repo{Dir: root, GitBin: binary}
	require.NoError(t, repo.InitWithObjectFormat(t.Context(), "sha1"))

	body, err := os.ReadFile(record)
	require.NoError(t, err)
	require.Contains(t, strings.Split(string(body), "\n"), "--object-format=sha1")
}
