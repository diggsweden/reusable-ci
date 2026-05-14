// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package manifest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
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

func TestSink_Write_RejectsEmptyStage(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	s := manifest.New(fsys.Root)
	if err := s.Write(context.Background(), "", nil); err == nil {
		t.Error("empty stage should error")
	}
}
