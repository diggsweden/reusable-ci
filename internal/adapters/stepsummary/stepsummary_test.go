// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package stepsummary_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/stepsummary"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
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
