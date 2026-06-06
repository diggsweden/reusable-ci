// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// SBOMCountStatusInput drives SBOMCountStatus.
type SBOMCountStatusInput struct {
	Kind    string
	Outcome string
	WorkDir string
}

// SBOMCountStatus appends SBOM status for workflows that generate one
// bom.json per artifact rather than a single fixed file. Same shape as
// BuildSBOMStatus: the Outcome value here is informational — the
// pass/fail verdict is owned by the SBOM step's own exit code.
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

	if in.Outcome == string(domainsummary.ResultSuccess) {
		count := countBOMFiles(workDir, preset)
		if count > 0 {
			_, _ = fmt.Fprintf(&b, "- ✅ CycloneDX: %d bom.json file(s)\n", count)
		} else {
			_, _ = fmt.Fprintf(&b, "- ⚠️ %s\n", preset.missingMessage)
		}
	} else {
		_, _ = fmt.Fprintf(&b, "- ❌ Generation step did not succeed — release blocked\n")
	}

	return sink.Append(ctx, b.String())
}

type sbomCountStatusPreset struct {
	title           string
	baseSuffix      string
	excludeContains string
	missingMessage  string
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
			title:           "### Build SBOM",
			excludeContains: "/target/",
			missingMessage:  "cargo-cyclonedx reported success but produced no bom.json",
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

	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Skip per-entry errors; the count reflects whichever bom.json
		// files the walker could read.
		if err != nil || d.IsDir() || filepath.Base(path) != "bom.json" {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		if preset.excludeContains != "" && strings.Contains(filepath.ToSlash(path), preset.excludeContains) {
			return nil
		}

		count++

		return nil
	})

	return count
}
