// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// Most guards in this package sweep `.github/workflows` and filter on `.yml`.
// GitHub accepts `.yaml` equally, so each of those filters is a silent
// assumption: correct today, and correct only because nobody has added a
// `.yaml` file. The failure it would cause is the worst kind — the guard keeps
// passing, having looked at one file fewer.
//
// Rather than edit nine filters into agreement and hope the tenth remembers,
// this states the assumption once and fails the day it stops holding. Whoever
// adds the first `.yaml` workflow gets told which filters to widen, at the
// moment it matters.
func TestWorkflowSweepsCoverEveryWorkflowFile(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var (
		yml     int
		unswept []string
	)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		switch {
		case strings.HasSuffix(entry.Name(), ".yml"):
			yml++
		case strings.HasSuffix(entry.Name(), ".yaml"):
			unswept = append(unswept, entry.Name())
		}
	}

	if yml < 20 {
		t.Fatalf("found %d workflow files; the directory, not the filter, is what was measured", yml)
	}

	sort.Strings(unswept)

	if len(unswept) > 0 {
		t.Errorf(".yaml workflows are present but most guards in this package filter on .yml only: %v\n"+
			"Switch those filters to isWorkflowFile (artifactdownload_guard_test.go) so the sweeps see these too. "+
			"Until then every guard here is reporting on a subset of the workflows without saying so.", unswept)
	}
}
