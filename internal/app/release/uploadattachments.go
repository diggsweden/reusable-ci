// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// UploadAttachmentsInput drives UploadAttachments.
type UploadAttachmentsInput struct {
	// Tag is the release tag/name to attach to. Required.
	Tag string
	// Pattern is a comma-, newline-, or whitespace-separated list of glob
	// patterns. Each pattern is expanded relative to WorkingDir.
	Pattern string
	// WorkingDir is the directory glob expansion is rooted at. Defaults to
	// the current working directory.
	WorkingDir string
}

// UploadAttachments expands Pattern into a list of files and uploads each
// to the release identified by Tag. Per-file failures are logged as
// warnings (matching the bash's `|| printf warning` behavior) — the
// function returns nil when at least one upload succeeded and the rest
// were warnings. An empty pattern is a no-op.
//
// Path safety: each pattern is required to stay within WorkingDir. Patterns
// that resolve to absolute paths outside WorkingDir or contain `..` segments
// are rejected before any glob expansion runs.
func UploadAttachments(ctx context.Context, up provider.ReleaseAssetUploader, w io.Writer, annot output.Annotator, in UploadAttachmentsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if strings.TrimSpace(in.Tag) == "" {
		return fmt.Errorf("tag is required: %w", errs.ErrUsage)
	}

	patterns := splitAttachPatterns(in.Pattern)
	if len(patterns) == 0 {
		_, _ = fmt.Fprintln(w, "No attachment patterns configured; nothing to upload.")

		return nil
	}

	root := in.WorkingDir
	if root == "" {
		root = "."
	}

	files, err := expandAttachmentPatterns(root, patterns)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		_, _ = fmt.Fprintf(w, "No files matched patterns %q under %s; nothing to upload.\n", in.Pattern, root)

		return nil
	}

	var failed []string

	for _, file := range files {
		_, _ = fmt.Fprintf(w, "Uploading %s to release...\n", file)

		if err := up.UploadReleaseAsset(ctx, in.Tag, file); err != nil {
			annot.Warningf("Failed to upload %s: %v", file, err)
			failed = append(failed, file)
		}
	}

	if len(failed) == len(files) {
		return fmt.Errorf("all %d uploads failed: %w", len(files), errs.ErrDependencyUnavailable)
	}

	return nil
}

// isRegularFile reports whether path exists AND is a regular file
// (no symlinks, no dirs). Symlink-following is intentionally disabled
// to keep `--clobber` uploads from following a link out of the
// workspace.
func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}

// expandAttachmentPatterns expands each glob pattern relative to root and
// returns a sorted, deduplicated list of matching regular files. Patterns
// that reach outside root are rejected.
func expandAttachmentPatterns(root string, patterns []string) ([]string, error) {
	seen := make(map[string]struct{})

	for _, pattern := range patterns {
		if strings.Contains(pattern, "..") {
			return nil, fmt.Errorf("pattern %q contains \"..\": %w", pattern, errs.ErrValidation)
		}

		if filepath.IsAbs(pattern) {
			return nil, fmt.Errorf("pattern %q must be relative to the working directory: %w", pattern, errs.ErrValidation)
		}

		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w: %w", pattern, err, errs.ErrValidation)
		}

		for _, m := range matches {
			if !isRegularFile(m) {
				continue
			}

			seen[m] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}

	sort.Strings(out)

	return out, nil
}
