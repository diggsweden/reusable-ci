// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// FindContainerSBOMInput drives FindContainerSBOM.
type FindContainerSBOMInput struct {
	// Dir is the directory to search. Empty → cwd. Only the top level
	// is scanned.
	Dir string
}

// FindContainerSBOM looks for the first
// *-analyzed-container-sbom.spdx.json file at the top of Dir and emits
// its basename via `sbom-file` on the OutputSink. Errors when no
// matching file is found.
//
// Used by the reusable-ci analyzed-container publish path.
func FindContainerSBOM(ctx context.Context, sink ci.OutputSink, w, stderr io.Writer, annot output.Annotator, in FindContainerSBOMInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		var err error

		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}

	var match string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasSuffix(name, "-analyzed-container-sbom.spdx.json") {
			match = name

			break
		}
	}

	if match == "" {
		annot.Errorf("No container SBOM file found matching pattern *-analyzed-container-sbom.spdx.json")
		annot.Errorf("SBOM generation step may have failed")

		return fmt.Errorf("no container SBOM file found: %w", errs.ErrValidation)
	}

	if err := sink.Set(ctx, "sbom-file", filepath.Base(match)); err != nil {
		return fmt.Errorf("set sbom-file: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Found SBOM file: %s\n", match)

	return nil
}
