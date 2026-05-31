// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package manifest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestSink_WriteAndCollectJobs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sink := manifest.New(dir)
	ctx := context.Background()

	for _, env := range []summary.JobResultEnvelope{
		{Job: "nanolinter", Result: summary.ResultSuccess},
		{Job: "scan", Result: summary.ResultFailure},
	} {
		if err := sink.WriteJob(ctx, env.Job, env); err != nil {
			t.Fatal(err)
		}
	}

	// Records land under jobs/<job>.json, not in the stage-manifest root.
	if _, err := os.Stat(filepath.Join(dir, "jobs", "nanolinter.json")); err != nil {
		t.Fatalf("expected jobs/nanolinter.json: %v", err)
	}

	docs, err := sink.CollectJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(docs) != 2 {
		t.Fatalf("collected %d docs, want 2", len(docs))
	}

	got := make(map[string]summary.Result)

	for _, doc := range docs {
		env, err := summary.ParseJobResultEnvelope(doc)
		if err != nil {
			t.Fatalf("parse %s: %v", doc, err)
		}

		got[env.Job] = env.Result
	}

	if got["nanolinter"] != summary.ResultSuccess || got["scan"] != summary.ResultFailure {
		t.Errorf("collected = %+v", got)
	}
}

func TestSink_CollectJobs_MissingDirIsEmpty(t *testing.T) {
	t.Parallel()

	sink := manifest.New(filepath.Join(t.TempDir(), "nonexistent"))

	docs, err := sink.CollectJobs(context.Background())
	if err != nil {
		t.Fatalf("missing jobs dir should not error: %v", err)
	}

	if len(docs) != 0 {
		t.Errorf("docs = %v, want empty", docs)
	}
}

func TestSink_WriteJob_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	sink := manifest.New(t.TempDir())

	err := sink.WriteJob(context.Background(), "", summary.JobResultEnvelope{Result: summary.ResultSuccess})
	if err == nil {
		t.Fatal("expected error for empty job name")
	}
}
