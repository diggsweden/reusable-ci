// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ghaoutput_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ghaoutput"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// $GITHUB_OUTPUT is a path the RUNNER chooses and hands over in the
// environment. The sink appends to it, so whatever that path resolves to
// receives everything the command publishes — versions, digests, tags, release
// SHAs — appended to it in the runner's own format.
//
// Following a symlink there means writing all of that somewhere the runner
// never nominated, under a name a workflow step or a checked-out repository
// could have created first. The sink used to open the path directly; it shares
// cliio's hardened open now, which refuses when the final component is a link
// and leaves a real file untouched.
func TestSink_RefusesASymlinkedOutputPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	link := filepath.Join(dir, "output")

	const original = "PRIVATE=value\n"

	require.NoError(t, os.WriteFile(target, []byte(original), 0o600))
	require.NoError(t, os.Symlink(target, link))

	sink := ghaoutput.New(link)

	err := sink.Set(context.Background(), "digest", "sha256:abc")
	require.Error(t, err, "the sink wrote through a symlink to a file the runner never named")
	require.ErrorIs(t, err, errs.ErrValidation, "the refusal must be classified rather than an opaque OS error")

	body, readErr := os.ReadFile(target) //nolint:gosec // owned temporary file.
	require.NoError(t, readErr)
	require.Equal(t, original, string(body), "the link's target was appended to")
}

// The control: a real file still works, and repeated writes still append.
// Without it the refusal above is indistinguishable from a sink that stopped
// writing at all. The file starts with an earlier step's line: the runner
// shares one output file across every step of a job, so a sink that opened it
// with O_TRUNC would erase what the steps before it published.
func TestSink_StillAppendsToARealOutputFile(t *testing.T) {
	t.Parallel()

	const earlier = "EARLIER_STEP=kept\n"

	path := filepath.Join(t.TempDir(), "output")
	require.NoError(t, os.WriteFile(path, []byte(earlier), 0o600))

	sink := ghaoutput.New(path)

	require.NoError(t, sink.Set(context.Background(), "first", "1"))
	require.NoError(t, sink.Set(context.Background(), "second", "2"))

	body, err := os.ReadFile(path) //nolint:gosec // owned temporary file.
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(string(body), earlier), "an earlier step's output was truncated:\n%s", body)
	require.Contains(t, string(body), "first=1")
	require.Contains(t, string(body), "second=2")
}
