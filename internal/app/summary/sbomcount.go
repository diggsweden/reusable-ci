// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// SBOMCountStatusInput drives SBOMCountStatus.
type SBOMCountStatusInput struct {
	Kind    string
	Outcome string
	WorkDir string
}

// SBOMCountStatus appends SBOM status for workflows that generate one
// bom.json per artifact rather than a single fixed file. Same shape as
// BuildSBOMStatus. It reports and never gates: Outcome is the generate step's
// outcome, and the "release blocked" wording describes the calling workflow,
// where that step failing fails the job the release depends on. Any outcome
// other than success or skipped (cancelled, or a value the workflow did not
// set) is reported as a failure.
func SBOMCountStatus(ctx context.Context, sink ci.SummarySink, in SBOMCountStatusInput) error {
	preset, err := sbomCountPreset(in.Kind)
	if err != nil {
		return err
	}

	workDir := in.WorkDir
	if workDir == "" {
		workDir = "."
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "%s\n", preset.title)

	switch in.Outcome {
	case string(domainsummary.ResultSuccess):
		count := countBOMFiles(workDir, preset)
		if count > 0 {
			_, _ = fmt.Fprintf(&b, "- ✓ CycloneDX: %d bom.json file(s)\n", count)
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

type sbomCountStatusPreset struct {
	title      string
	baseSuffix string
	// excludeDir names directories whose contents are not counted, at any
	// depth below the walk root. They are skipped, not walked and filtered:
	// a Cargo target/ can hold far more files than the rest of the tree.
	excludeDir     string
	missingMessage string
}

func sbomCountPreset(kind string) (sbomCountStatusPreset, error) {
	// Switch on the canonical projecttype enum so a rename in
	// internal/domain/projecttype propagates here automatically.
	switch projecttype.Type(strings.TrimSpace(kind)) {
	case projecttype.Go:
		return sbomCountStatusPreset{
			title:          "### Go Build SBOM",
			baseSuffix:     ".reusable-ci/go-build-sbom",
			missingMessage: "cyclonedx-gomod reported success but produced no bom.json",
		}, nil
	case projecttype.Cargo:
		return sbomCountStatusPreset{
			title:          "### Build SBOM",
			excludeDir:     "target",
			missingMessage: "cargo-cyclonedx reported success but produced no bom.json",
		}, nil
	default:
		return sbomCountStatusPreset{}, fmt.Errorf("unsupported SBOM count kind %q: %w", kind, errs.ErrUsage)
	}
}

func countBOMFiles(workDir string, preset sbomCountStatusPreset) int {
	root := workDir
	if preset.baseSuffix != "" {
		root = filepath.Join(workDir, filepath.FromSlash(preset.baseSuffix))
	}

	// Per-entry errors are skipped, so an unreadable directory lowers the count
	// rather than failing the summary: the count is of the bom.json files the
	// walk could read.
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		if entry.IsDir() && path != root && preset.excludeDir != "" && entry.Name() == preset.excludeDir {
			return filepath.SkipDir
		}

		if entry.Type().IsRegular() && entry.Name() == "bom.json" {
			count++
		}

		return nil
	})

	return count
}
