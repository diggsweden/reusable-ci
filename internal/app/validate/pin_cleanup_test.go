// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failedPinClone struct{ dir string }

func (f *failedPinClone) Clone(_ context.Context, _, dir string) error {
	f.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "partial"), []byte("fixture"), 0o600); err != nil {
		return err
	}

	return errs.ErrDependencyUnavailable
}
func (*failedPinClone) Open(string) appvalidate.PinGitOps { panic("failed clone must not be opened") }

func TestPinCloneFailure_RemovesPartialState(t *testing.T) {
	t.Parallel()
	workflow := filepath.Join(t.TempDir(), "workflow.yml")
	require.NoError(t, os.WriteFile(workflow, []byte("jobs:\n  test:\n    uses: owner/workflows/path@"+strings.Repeat("a", 40)+"\n"), 0o600))
	root := t.TempDir()
	clone := &failedPinClone{}
	err := appvalidate.PinReachability(t.Context(), clone, io.Discard, appvalidate.PinReachabilityInput{Workflows: []string{workflow}, Subject: "owner/workflows", Remote: "https://fixture.invalid/repo", TempDir: root})
	require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
	require.NotEmpty(t, clone.dir)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}
