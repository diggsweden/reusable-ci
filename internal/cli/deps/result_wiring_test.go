// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package deps_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func TestBuild_ManifestAndJobResultWiring(t *testing.T) {
	const (
		stageBody = `{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"mobile":"failure"}}`
		jobBody   = `{"version":1,"job":"mobile-job","result":"cancelled"}`
	)

	for _, mode := range []string{"default", "override"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("REUSABLE_CI_PROVIDER", "local")
			t.Setenv("REUSABLE_CI_RUNNER", "local")
			t.Setenv("CI_RESULTS_DIR", "")

			root, err := os.Getwd()
			require.NoError(t, err)

			selected, unused := filepath.Join(root, ".ci-results"), filepath.Join(root, "owned-results")
			if mode == "override" {
				selected, unused = unused, selected
				t.Setenv("CI_RESULTS_DIR", selected)
			}

			require.NoError(t, os.Mkdir(unused, 0o700))
			canary := filepath.Join(unused, "canary")
			require.NoError(t, os.WriteFile(canary, []byte("unselected storage"), 0o600))
			before, err := os.Stat(canary)
			require.NoError(t, err)

			ctx := t.Context()
			built, err := deps.Build(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, built.Close(ctx)) })
			require.NotNil(t, built.ManifestSink)
			require.NotNil(t, built.JobResultStore)
			require.NoDirExists(t, selected, "construction must not create result storage")
			require.NoError(t, built.ManifestSink.WriteJSON(ctx, "build", json.RawMessage(stageBody)))
			require.NoError(t, built.JobResultStore.WriteJob(ctx, "mobile-job", json.RawMessage(jobBody)))

			stage, err := os.ReadFile(filepath.Join(selected, "build-result.json"))
			require.NoError(t, err)
			require.True(t, bytes.Equal(stage, []byte(stageBody+"\n")), "stage bytes or trailing newline changed")

			job, err := os.ReadFile(filepath.Join(selected, "jobs", "mobile-job.json"))
			require.NoError(t, err)
			require.True(t, bytes.Equal(job, []byte(jobBody+"\n")), "job bytes or trailing newline changed")

			docs, err := built.JobResultStore.CollectJobs(ctx)
			require.NoError(t, err)
			require.Equal(t, [][]byte{[]byte(jobBody + "\n")}, docs, "collection must exclude the stage manifest")

			entries, err := os.ReadDir(selected)
			require.NoError(t, err)
			require.Len(t, entries, 2)
			require.Equal(t, "build-result.json", entries[0].Name())
			require.Equal(t, "jobs", entries[1].Name())
			require.True(t, entries[1].IsDir())
			entries, err = os.ReadDir(filepath.Join(selected, "jobs"))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.Equal(t, "mobile-job.json", entries[0].Name())

			entries, err = os.ReadDir(unused)
			require.NoError(t, err)
			require.Len(t, entries, 1, "writes reached the unselected result root")
			require.Equal(t, "canary", entries[0].Name())

			body, err := os.ReadFile(canary)
			require.NoError(t, err)
			require.Equal(t, "unselected storage", string(body))

			after, err := os.Stat(canary)
			require.NoError(t, err)
			require.Equal(t, before.Mode(), after.Mode())
			require.True(t, os.SameFile(before, after))
		})
	}
}
