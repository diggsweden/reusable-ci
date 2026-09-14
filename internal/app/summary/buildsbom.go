// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// BuildSBOMStatusInput drives BuildSBOMStatus.
type BuildSBOMStatusInput struct {
	Ecosystem string
	Outcome   string
	WorkDir   string
}

// BuildSBOMStatus appends the standard Build SBOM summary block to the
// step summary, locating the produced files via the ecosystem preset
// (paths + filename patterns). Explicitly disabled generation is a supported
// opt-out, while a failed enabled generation blocks the release.
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

	switch in.Outcome {
	case string(domainsummary.ResultSuccess):
		bom := locateBuildSBOM(workDir, preset)
		if bom != "" {
			_, _ = fmt.Fprintf(&b, "- ✓ %s: `%s`\n", preset.successLabel, bom)
		} else {
			_, _ = fmt.Fprintf(&b, "- ⚠️ %s\n", preset.missingMessage)
		}
	case string(domainsummary.ResultSkipped):
		_, _ = fmt.Fprintf(&b, "- ⊘ Generation disabled; release continues without a build SBOM\n")
	default:
		_, _ = fmt.Fprintf(&b, "- ⚠️ Generation failed; release blocked until the Build SBOM succeeds or is explicitly disabled\n")
	}

	return sink.Append(ctx, b.String())
}

type buildSBOMStatusPreset struct {
	// patterns are slash paths relative to the working directory, tried in
	// order when modules is false.
	patterns []string
	// modules also accepts a pattern below a module directory, as Gradle
	// writes app/build/reports/...; without it only the exact paths count, so
	// an npm dependency's bom.json or a Maven module's target/bom.json is never
	// reported as the project's SBOM.
	modules        bool
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
			modules:        true,
			successLabel:   "CycloneDX",
			missingMessage: "Generated but file not located - check plugin output path",
		}, nil
	case projecttype.GradleAndroid:
		return buildSBOMStatusPreset{
			patterns:       []string{"build/reports/bom.json", "build/reports/cyclonedx/bom.json"},
			modules:        true,
			successLabel:   "CycloneDX",
			missingMessage: "Generated but file not located - check plugin output path",
		}, nil
	default:
		return buildSBOMStatusPreset{}, fmt.Errorf("unsupported build SBOM ecosystem %q: %w", ecosystem, errs.ErrUsage)
	}
}

// locateBuildSBOM returns the SBOM the preset names under root, or "" when
// there is none. Only regular files count, so a dangling or redirecting
// symlink is not reported as an SBOM.
func locateBuildSBOM(root string, preset buildSBOMStatusPreset) string {
	if !preset.modules {
		for _, pattern := range preset.patterns {
			path := filepath.Join(root, filepath.FromSlash(pattern))
			if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
				return path
			}
		}

		return ""
	}

	return shallowestModuleMatch(root, preset.patterns)
}

// shallowestModuleMatch walks root for the patterns at any module depth and
// prefers the fewest module directories, then the path, so the root project's
// report wins over a module's and the choice does not depend on walk order.
func shallowestModuleMatch(root string, patterns []string) string {
	type match struct {
		path  string
		depth int
	}

	var matches []match

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Skip per-entry errors; the walker accumulates whichever
		// matches it could read.
		if err != nil || !d.Type().IsRegular() {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr // a path outside root is not a candidate
		}

		slashRel := filepath.ToSlash(rel)
		for _, suffix := range patterns {
			if slashRel == suffix || strings.HasSuffix(slashRel, "/"+suffix) {
				// Depth counts the module directories before the pattern, so a
				// root report under a longer pattern still beats a module's.
				matches = append(matches, match{path: path, depth: strings.Count(strings.TrimSuffix(slashRel, suffix), "/")})

				break
			}
		}

		return nil
	})

	if len(matches) == 0 {
		return ""
	}

	slices.SortFunc(matches, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.depth, b.depth), strings.Compare(a.path, b.path))
	})

	return matches[0].path
}
