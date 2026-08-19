// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package manifest_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestSink_Write_CreatesDir(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Path("subdir", "nested")
	s := manifest.New(dir)

	err := s.Write(context.Background(), "build", map[string]any{
		"stage": "build", "result": "success", "ran": true,
	})
	if err != nil {
		t.Fatal(err)
	}

	data := fsys.ReadFile("subdir/nested/build-result.json")

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got["stage"] != "build" || got["result"] != "success" {
		t.Errorf("got %v", got)
	}
}

type rawBody string

func (r rawBody) MarshalJSON() ([]byte, error) { return []byte(r), nil }

func TestSink_WriteJSON_PreservesBody(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	s := manifest.New(dir)

	body := `{"stage":"build","result":"success","ran":true,"targets":{"npm":"success","maven":"skipped"}}`
	if err := s.WriteJSON(context.Background(), "build", rawBody(body)); err != nil {
		t.Fatal(err)
	}

	got := fsys.ReadFile("build-result.json")
	if !strings.HasPrefix(string(got), body) {
		t.Errorf("body not preserved: %s", got)
	}
}

func TestNewFromEnv_DefaultsToCIResultsDir(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("CI_RESULTS_DIR", "")

	s := manifest.NewFromEnv()
	if s.Dir != ".ci-results" {
		t.Errorf("Dir = %q", s.Dir)
	}
}

func TestNewFromEnv_HonoursOverride(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("CI_RESULTS_DIR", "/custom/dir")

	s := manifest.NewFromEnv()
	if s.Dir != "/custom/dir" {
		t.Errorf("Dir = %q", s.Dir)
	}
}

func TestSink_RejectsEmptyStage(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	s := manifest.New(fsys.Root)
	ctx := context.Background()

	// Both write paths. The stage name becomes the filename, so an empty
	// one would write "-result.json" -- a file no consumer globs for and
	// that every stage would then overwrite in turn.
	if err := s.Write(ctx, "", nil); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("Write err = %v, want ErrUsage", err)
	}

	if err := s.WriteJSON(ctx, "", jsonBody(`{}`)); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("WriteJSON err = %v, want ErrUsage", err)
	}

	// Refused before the directory is made, so a bad call leaves nothing.
	entries, err := os.ReadDir(fsys.Root)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("refused writes left %d entries behind", len(entries))
	}
}

// jsonBody is a minimal json.Marshaler for driving WriteJSON.
type jsonBody string

func (b jsonBody) MarshalJSON() ([]byte, error) { return []byte(b), nil }
