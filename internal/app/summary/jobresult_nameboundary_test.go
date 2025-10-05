// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/manifest"
	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
	"github.com/stretchr/testify/require"
)

func TestJobResult_NameBoundaryRefusesBeforeStore(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"", " \t\n", ".", "..", "../../canary", "nested/job", `nested\job`,
		"job\x00name", "job\tname", "job\nname", "job\rname", "job\x1bname",
		"job\x7fname", "job\u0085name", "job\xffname",
	} {
		t.Run(name, func(t *testing.T) {
			store := &nameBoundaryStore{Store: fakejobresultstore.New(t)}
			err := appsummary.JobResult(t.Context(), store, appsummary.JobResultInput{Job: name, Status: "success"})
			require.ErrorIs(t, err, errs.ErrUsage)
			require.NotErrorIs(t, err, errs.ErrValidation)
			require.Zero(t, store.calls, "invalid name reached the store")
		})
	}
}

func TestJobResult_NameBoundaryPreservesNamesAfterOuterTrim(t *testing.T) {
	t.Parallel()
	store := fakejobresultstore.New(t)

	names := []string{"Build", "build", "build linux", "build-linux", "build_linux", "matrix (go=1.26, os=linux) [a+b]@v1:ok", "R\u00e4ksm\u00f6rg\u00e5s \u65e5\u672c", "v1..2", ".hidden", "-"}
	for _, name := range names {
		err := appsummary.JobResult(t.Context(), store, appsummary.JobResultInput{Job: " \t\n" + name + "\r\n ", Status: "success"})
		require.NoError(t, err)

		var got struct {
			Job    string `json:"job"`
			Result string `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(store.Body(name)), &got))
		require.Equal(t, name, got.Job)
		require.Equal(t, "success", got.Result)
	}

	docs, err := store.CollectJobs(t.Context())
	require.NoError(t, err)
	require.Len(t, docs, len(names), "distinct names must not be sanitized into collisions")
}

func TestJobResult_NameBoundaryContainsConcreteManifestEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	canary := filepath.Join(root, "canary.json")
	require.NoError(t, os.WriteFile(canary, []byte("original\n"), 0o600))
	before, err := os.Lstat(canary)
	require.NoError(t, err)

	results := filepath.Join(root, "results")
	err = appsummary.JobResult(t.Context(), manifest.New(results), appsummary.JobResultInput{Job: "../../canary", Status: "success"})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.NotErrorIs(t, err, errs.ErrValidation)
	body, err := os.ReadFile(canary)
	require.NoError(t, err)
	require.Equal(t, "original\n", string(body))

	after, err := os.Lstat(canary)
	require.NoError(t, err)
	require.Equal(t, before.Mode(), after.Mode())
	require.True(t, os.SameFile(before, after))

	_, err = os.Lstat(results)
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

type nameBoundaryStore struct {
	*fakejobresultstore.Store
	calls int
}

func (s *nameBoundaryStore) WriteJob(ctx context.Context, name string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	s.calls++

	return s.Store.WriteJob(ctx, name, body)
}
