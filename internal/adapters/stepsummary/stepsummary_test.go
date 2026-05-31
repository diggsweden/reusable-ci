// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package stepsummary_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/stepsummary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestSink_AppendsMarkdown(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	path := fsys.Path("summary.md")

	s := stepsummary.New(path)
	if err := s.Append(context.Background(), "## title\n"); err != nil {
		t.Fatal(err)
	}

	if err := s.Append(context.Background(), "second line\n"); err != nil {
		t.Fatal(err)
	}

	data := fsys.ReadFile("summary.md")
	if !strings.Contains(string(data), "## title") || !strings.Contains(string(data), "second line") {
		t.Errorf("file content = %q", data)
	}
}

func TestSink_NoopWhenEmptyPath(t *testing.T) {
	t.Parallel()

	s := stepsummary.New("")
	if err := s.Append(context.Background(), "would-have-written"); err != nil {
		t.Errorf("empty-path Append should be a no-op, got %v", err)
	}
}

// NewWithLog (the Forgejo path): no summary file → echo to the job log
// rather than dropping the content silently.
func TestSink_WithLog_EchoesToLogWhenNoFile(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer

	s := stepsummary.NewWithLog("", &log)
	if err := s.Append(context.Background(), "## forgejo summary\n"); err != nil {
		t.Fatal(err)
	}

	if got := log.String(); got != "## forgejo summary\n" {
		t.Errorf("log = %q, want the summary markdown", got)
	}
}

// When a summary file IS set, NewWithLog writes the file and leaves the log
// untouched (no double-emit).
func TestSink_WithLog_PrefersFileOverLog(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	path := fsys.Path("summary.md")

	var log bytes.Buffer

	s := stepsummary.NewWithLog(path, &log)
	if err := s.Append(context.Background(), "## to file\n"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(fsys.ReadFile("summary.md")), "## to file") {
		t.Errorf("summary file missing content")
	}

	if log.Len() != 0 {
		t.Errorf("log should be empty when a file is configured, got %q", log.String())
	}
}
