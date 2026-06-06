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
)

// ExtractedBinariesInput drives ExtractedBinaries.
type ExtractedBinariesInput struct {
	Dir           string
	ArtifactName  string
	DisplayName   string
	ExtractTarget string
	ExpectedNames string
	Platform      string
	Limit         int
}

// ExtractedBinaries appends the extracted-binary summary table to the
// step summary, with a bounded file listing (`Limit`, default 50) so a
// runaway extract stage doesn't produce a step summary GHA refuses to
// render.
func ExtractedBinaries(ctx context.Context, sink ci.SummarySink, in ExtractedBinariesInput) error {
	dir := in.Dir
	if dir == "" {
		dir = "./extracted-binaries"
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "### Extracted Binaries - %s (%s)\n\n", in.DisplayName, in.Platform)
	_, _ = fmt.Fprintf(&b, "- **Stage:** %s\n", in.ExtractTarget)
	_, _ = fmt.Fprintf(&b, "- **Artefact:** %s\n", in.ArtifactName)
	_, _ = fmt.Fprintf(&b, "- **Platform:** %s\n", in.Platform)

	if strings.TrimSpace(in.ExpectedNames) != "" {
		_, _ = fmt.Fprintf(&b, "- **Binaries:** %s\n", in.ExpectedNames)
	}

	_, _ = fmt.Fprintf(&b, "\nFiles in extracted-binaries/:\n\n")

	for _, path := range firstFiles(dir, limit) {
		_, _ = fmt.Fprintf(&b, "%s\n", path)
	}

	return sink.Append(ctx, b.String())
}

func firstFiles(root string, limit int) []string {
	var files []string

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Skip per-entry errors; the function returns whichever files
		// it could read up to the caller's limit.
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		files = append(files, path)

		return nil
	})

	sort.Strings(files)

	if len(files) > limit {
		return files[:limit]
	}

	return files
}
