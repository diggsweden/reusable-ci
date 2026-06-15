// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// BuildSBOMStatusInput drives BuildSBOMStatus.
type BuildSBOMStatusInput struct {
	Ecosystem string
	Outcome   string
	WorkDir   string
}

// BuildSBOMStatus appends the standard Build SBOM summary block to the
// step summary, locating the produced files via the ecosystem preset
// (paths + filename patterns). The Outcome value here is informational
// only — the actual workflow gate is enforced by the SBOM step having
// no `continue-on-error`, so a missing SBOM fails the build before this
// summary writer runs.
func BuildSBOMStatus(ctx context.Context, sink ci.SummarySink, in BuildSBOMStatusInput) error {
	preset, err := buildSBOMPreset(in.Ecosystem)
	if err != nil {
		return err
	}

	workDir := in.WorkDir
	if workDir == "" {
		workDir = "."
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "### Build SBOM\n")

	if in.Outcome == string(domainsummary.ResultSuccess) {
		bom := firstMatchingPath(workDir, preset.patterns)
		if bom != "" {
			_, _ = fmt.Fprintf(&b, "- ✓ %s: `%s`\n", preset.successLabel, bom)
		} else {
			_, _ = fmt.Fprintf(&b, "- ⚠️ %s\n", preset.missingMessage)
		}
	} else {
		// The SBOM step is mandatory — a non-success outcome means the
		// workflow has already failed before this summary block ran (the
		// status report is invoked under `if: always()` so it surfaces
		// the failure in the step summary too).
		_, _ = fmt.Fprintf(&b, "- ✗ Generation step did not succeed — release blocked\n")
	}

	return sink.Append(ctx, b.String())
}

type buildSBOMStatusPreset struct {
	patterns       []string
	successLabel   string
	missingMessage string
}

func buildSBOMPreset(ecosystem string) (buildSBOMStatusPreset, error) {
	// Switch on the canonical projecttype enum so a rename in
	// internal/domain/projecttype propagates here automatically.
	switch projecttype.Type(strings.TrimSpace(ecosystem)) {
	case projecttype.NPM:
		return buildSBOMStatusPreset{
			patterns:       []string{"bom.json"},
			successLabel:   "CycloneDX", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			missingMessage: "cyclonedx-npm reported success but produced no bom.json",
		}, nil
	case projecttype.Maven:
		return buildSBOMStatusPreset{
			patterns:       []string{"target/bom.json"},
			successLabel:   "CycloneDX (aggregate)",
			missingMessage: "Plugin ran but no bom.json was written",
		}, nil
	case projecttype.Gradle:
		return buildSBOMStatusPreset{
			patterns:       []string{"build/reports/bom.json", "build/reports/cyclonedx/bom.json"},
			successLabel:   "CycloneDX",
			missingMessage: "Generated but file not located - check plugin output path",
		}, nil
	case projecttype.GradleAndroid:
		return buildSBOMStatusPreset{
			patterns:       []string{"build/reports/bom.json"},
			successLabel:   "CycloneDX",
			missingMessage: "Generated but file not located - check plugin output path",
		}, nil
	default:
		return buildSBOMStatusPreset{}, fmt.Errorf("unsupported build SBOM ecosystem %q: %w", ecosystem, errs.ErrUsage)
	}
}

func firstMatchingPath(root string, patterns []string) string {
	var matches []string

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Skip per-entry errors; the walker accumulates whichever
		// matches it could read.
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		slashPath := filepath.ToSlash(path)
		for _, suffix := range patterns {
			if slashPath == suffix || strings.HasSuffix(slashPath, "/"+suffix) {
				matches = append(matches, path)

				break
			}
		}

		return nil
	})

	sort.Strings(matches)

	if len(matches) == 0 {
		return ""
	}

	return matches[0]
}
