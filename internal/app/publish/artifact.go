// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// FindArtifactInput drives FindArtifact.
type FindArtifactInput struct {
	Dir       string
	Ext       string
	Exts      []string
	OutputKey string
	Label     string
	Recursive bool
}

// FindArtifact finds the first matching file under Dir, emits it as OutputKey,
// and prints a short log line for workflow visibility.
func FindArtifact(ctx context.Context, sink ci.OutputSink, w io.Writer, annot output.Annotator, in FindArtifactInput) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		dir = "artifacts"
	}

	exts := normalizeArtifactExtensions(in.Ext, in.Exts)
	if len(exts) == 0 {
		return "", fmt.Errorf("artifact extension is required: %w", errs.ErrUsage)
	}

	label := in.Label
	if label == "" {
		label = strings.ToUpper(strings.TrimPrefix(exts[0], "."))
	}

	if in.OutputKey == "" {
		return "", fmt.Errorf("artifact output key is required: %w", errs.ErrUsage)
	}

	matches, err := findArtifactsByExts(dir, exts, in.Recursive)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		annot.Errorf("No %s file found in artifact", label)

		return "", fmt.Errorf("no %s file found under %s: %w", label, dir, errs.ErrMissingInput)
	}

	selected := matches[0]
	if len(matches) > 1 {
		annot.Warningf("Multiple %s files found; using %s", label, selected)
	}

	if err := sink.Set(ctx, in.OutputKey, selected); err != nil {
		return "", fmt.Errorf("set %s: %w", in.OutputKey, err)
	}

	_, _ = fmt.Fprintf(w, "Found %s: %s\n", label, selected)

	if info, err := os.Stat(selected); err == nil {
		_, _ = fmt.Fprintf(w, "Size: %d bytes\n", info.Size())
	}

	return selected, nil
}

func normalizeArtifactExtensions(ext string, exts []string) []string {
	var out []string

	for _, candidate := range append([]string{ext}, exts...) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		if !strings.HasPrefix(candidate, ".") {
			candidate = "." + candidate
		}

		out = append(out, strings.ToLower(candidate))
	}

	return out
}

//nolint:cyclop // rooted walk, recursion, extension matching and file-type refusal are separate guards.
func findArtifactsByExts(dir string, exts []string, recursive bool) ([]string, error) {
	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	var matches []string

	if err := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if !recursive && path != "." {
				return filepath.SkipDir
			}

			return nil
		}

		lowerPath := strings.ToLower(path)
		for _, ext := range exts {
			if strings.HasSuffix(lowerPath, ext) {
				info, statErr := root.Lstat(path)
				if statErr != nil {
					return statErr
				}

				if !info.Mode().IsRegular() {
					return fmt.Errorf("artifact must be a confined regular file: %w", errs.ErrValidation)
				}

				matches = append(matches, filepath.Join(dir, path))

				break
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("find artifact files under %s: %w", dir, err)
	}

	sort.Strings(matches)

	return matches, nil
}
