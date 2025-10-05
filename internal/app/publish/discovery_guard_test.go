// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

func TestFindArtifact_RejectsLinkedFilesBeforeOutputs(t *testing.T) {
	t.Parallel()

	for _, recursive := range []bool{false, true} {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.jar")
		require.NoError(t, os.WriteFile(outside, []byte("canary"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "app.jar")))
		sink := fakeoutputsink.New(t)

		var log bytes.Buffer

		got, err := apppublish.FindArtifact(t.Context(), sink, &log, output.Annotator{}, apppublish.FindArtifactInput{Dir: root, Ext: "jar", OutputKey: "artifact", Recursive: recursive})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Empty(t, got)
		require.Empty(t, sink.Keys())
		require.Empty(t, log.String())

		body, err := os.ReadFile(outside)
		require.NoError(t, err)
		require.Equal(t, "canary", string(body))
	}
}

func TestMavenValidateArtifacts_RequiresMetadataPerModule(t *testing.T) {
	t.Parallel()

	for _, complete := range []bool{false, true} {
		root := t.TempDir()
		for _, module := range []string{"first", "second"} {
			dir := filepath.Join(root, module, "target")
			require.NoError(t, os.MkdirAll(dir, 0o700))

			for _, kind := range []string{"sources", "javadoc"} {
				if !complete && ((module == "first" && kind == "javadoc") || (module == "second" && kind == "sources")) {
					continue
				}

				require.NoError(t, os.WriteFile(filepath.Join(dir, module+"-"+kind+".jar"), []byte("fixture"), 0o600))
			}
		}

		_, err := apppublish.MavenValidateArtifacts(t.Context(), io.Discard, io.Discard, output.Annotator{}, apppublish.MavenValidateArtifactsInput{Root: root})
		if complete {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, errs.ErrValidation)
		}
	}
}
