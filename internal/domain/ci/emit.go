// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci

import (
	"context"
	"errors"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// MultilineEntry is one named multi-line step output.
type MultilineEntry struct {
	Key   string
	Lines []string
}

// EmitMultiline writes multi-line step outputs portably, keeping callers
// forge-blind. Where the OutputSink encodes multi-line values natively
// (GitHub/Forgejo: the $*_OUTPUT heredoc) each entry is written to the
// step-output file. Where it cannot — GitLab, whose dotenv report holds no
// newlines and returns ErrUnsupported — the whole set is written once to the
// stage manifest ($CI_RESULTS_DIR/<stage>-result.json), the portable
// cross-job channel. Document-like values (a changelog, a tag list) then
// travel as an artifact file, never as CI/CD environment variables.
//
// The OutputSink is attempted first. The GitLab sink reports ErrUnsupported
// before writing anything, so the manifest fallback re-emits every entry with
// no risk of a partial write split across the two channels. A sink that does
// support multi-line never touches the manifest, so `manifest` may be nil on
// those runners.
func EmitMultiline(ctx context.Context, out OutputSink, manifest ManifestSink, stage string, entries ...MultilineEntry) error {
	for _, entry := range entries {
		err := out.SetMultiline(ctx, entry.Key, entry.Lines)
		if err == nil {
			continue
		}

		if !errors.Is(err, errs.ErrUnsupported) {
			return err
		}

		return emitMultilineManifest(ctx, manifest, stage, entries)
	}

	return nil
}

// emitMultilineManifest writes every entry into one stage-result document so
// a multi-key caller (e.g. container metadata's tags + labels) never
// overwrites its own earlier keys.
func emitMultilineManifest(ctx context.Context, manifest ManifestSink, stage string, entries []MultilineEntry) error {
	if manifest == nil {
		return fmt.Errorf("ci: multi-line output for stage %q needs a manifest sink on this runner: %w", stage, errs.ErrUnsupported)
	}

	result := make(map[string]any, len(entries))
	for _, entry := range entries {
		result[entry.Key] = entry.Lines
	}

	return manifest.Write(ctx, stage, result)
}
