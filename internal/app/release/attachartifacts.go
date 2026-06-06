// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// AttachArtifactsInput drives `release attach-artifacts`.
type AttachArtifactsInput struct {
	UserAttach   string
	BinariesDir  string
	BinariesGlob string
}

// AttachArtifacts emits the effective release attachment glob list.
func AttachArtifacts(ctx context.Context, sink ci.OutputSink, w io.Writer, in AttachArtifactsInput) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	binariesDir := in.BinariesDir
	if binariesDir == "" {
		binariesDir = domainrelease.DefaultReleaseBinariesDir
	}

	binariesGlob := in.BinariesGlob
	if binariesGlob == "" {
		binariesGlob = domainrelease.DefaultReleaseBinariesGlob
	}

	patterns := splitAttachPatterns(in.UserAttach)
	if hasFiles(binariesDir) {
		if binariesGlob != "" && !containsPattern(patterns, binariesGlob) {
			patterns = append(patterns, binariesGlob)
		}
	}

	effective := strings.Join(patterns, ",")
	if err := sink.Set(ctx, "attach-artifacts", effective); err != nil {
		return "", err
	}

	if w != nil {
		_, _ = fmt.Fprintln(w, effective)
	}

	return effective, nil
}

func splitAttachPatterns(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		out = append(out, part)
	}

	return out
}

func containsPattern(patterns []string, want string) bool {
	for _, got := range patterns {
		if got == want {
			return true
		}
	}

	return false
}

func hasFiles(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		// Skip per-entry errors (permissions, race-deleted files) and
		// keep walking — `hasFiles` only needs to find one entry.
		if err != nil || d == nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		found = true

		return os.ErrExist
	})

	return found
}
