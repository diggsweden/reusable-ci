// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DigestInput drives Digest.
type DigestInput struct {
	// Dir is the directory whose contents are digested.
	Dir string
	// FS overrides filesystem access for tests. Nil -> the real OS filesystem,
	// rooted at Dir.
	FS fs.FS
}

// Digest prints the canonical content digest of a directory (64 hex chars +
// newline) to out. For the build->sign hand-off use `release dist-digest` and
// `release validate-dist` instead; that pair, not this one, is wired into the
// cross-job tamper-evidence channel. See internal/domain/artifact.Digest.
func Digest(out io.Writer, in DigestInput) error {
	fsys := in.FS
	root := in.Dir

	if fsys == nil {
		if in.Dir == "" {
			return fmt.Errorf("artifact digest: --dir is required: %w", errs.ErrUsage)
		}

		fsys = os.DirFS(in.Dir)
		root = "."
	}

	sum, err := domainartifact.Digest(fsys, root)
	if err != nil {
		return fmt.Errorf("digest %s: %w", in.Dir, err)
	}

	_, _ = fmt.Fprintln(out, sum)

	return nil
}
