// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
)

// FileSigner signs one SBOM file, emitting a detached signature sidecar
// (cosign produces <file>.bundle). Satisfied by app/release.CosignSigner, so
// `sbom assemble --sign` reuses the same signing path as `release sign` — this
// package stays free of cosign/adapter imports.
type FileSigner interface {
	SignFile(ctx context.Context, file string) error
}

// SignAssembled signs every assembled SBOM under dir — files matching
// *-sbom.spdx.json / *-sbom.cyclonedx.json — producing a <file>.bundle each.
// Mode-agnostic: it runs after any assemble mode and signs whatever landed.
func SignAssembled(ctx context.Context, signer FileSigner, dir string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names (w) — testing/http/io conventions.
	files, err := findAssembledSBOMs(dir)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		_, _ = fmt.Fprintln(w, "🔏 Signing: no assembled SBOMs found, nothing to sign")

		return nil
	}

	_, _ = fmt.Fprintln(w, "🔏 Signing assembled SBOMs...")

	for _, file := range files {
		if signErr := signer.SignFile(ctx, file); signErr != nil {
			return fmt.Errorf("sign %s: %w", file, signErr)
		}

		_, _ = fmt.Fprintf(w, "   %s %s.bundle\n", clicolor.Check(w), file)
	}

	return nil
}

// findAssembledSBOMs walks dir for canonical SBOM outputs, skipping any sidecar
// bundles. Sorted for deterministic signing order.
func findAssembledSBOMs(dir string) ([]string, error) {
	if dir == "" {
		dir = "."
	}

	var out []string

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err //nolint:wrapcheck // walker error surfaced as-is.
		}

		name := d.Name()
		if strings.HasSuffix(name, "-sbom.spdx.json") || strings.HasSuffix(name, "-sbom.cyclonedx.json") {
			out = append(out, path)
		}

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan %q for SBOMs: %w", dir, walkErr)
	}

	sort.Strings(out)

	return out, nil
}
