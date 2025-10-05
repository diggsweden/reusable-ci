// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// Signing blobs are small credentials, not unbounded artifact archives.
const maxMobileSecretBytes = 16 << 20

// OS temp roots can contain system aliases (notably /var on macOS). Resolve
// that default once; explicit secret destinations still reject every link.
func mobileTempRoot() (string, error) {
	root, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve OS temporary directory: %w", err)
	}

	return root, nil
}

func decodeMobileSecret(encoded string) ([]byte, error) {
	if len(encoded) > 2*maxMobileSecretBytes {
		return nil, fmt.Errorf("encoded signing material exceeds size limit: %w", errs.ErrMalformedInput)
	}

	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil || len(body) == 0 || len(body) > maxMobileSecretBytes {
		return nil, fmt.Errorf("signing material must be nonempty base64 of at most 16 MiB: %w", errs.ErrMalformedInput)
	}

	return body, nil
}

// Replace through the shared rooted installer: neither existing leaf links nor
// parent links are followed, and replacement does not retain an old broad mode.
func writeMobileSecret(path string, body []byte) (err error) {
	stage, err := pathsafe.NewArtifactStaging(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, stage.Close()) }()

	if err := stage.Root().WriteFile(filepath.Base(path), body, 0o600); err != nil {
		return err
	}

	return stage.Install()
}
