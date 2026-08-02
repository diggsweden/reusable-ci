// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
)

func TestParseLintEngine(t *testing.T) {
	t.Parallel()

	cases := map[string]pipeline.LintEngine{
		"nanolinter":   pipeline.LintEngineNanolinter,
		"  MegaLinter": pipeline.LintEngineMegalinter, // case- and space-insensitive
		"none":         pipeline.LintEngineNone,
		"":             pipeline.LintEngineNone, // empty defaults to none — no consumer linter baked in
	}
	for in, want := range cases {
		got, err := pipeline.ParseLintEngine(in)
		if err != nil {
			t.Errorf("ParseLintEngine(%q) errored: %v", in, err)
		}

		if got != want {
			t.Errorf("ParseLintEngine(%q) = %q, want %q", in, got, want)
		}
	}

	if _, err := pipeline.ParseLintEngine("eslint"); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("unknown engine should be a usage error, got %v", err)
	}
}
