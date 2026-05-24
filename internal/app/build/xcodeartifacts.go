// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// XcodeListBuiltArtifacts prints the .ipa / .xcarchive paths under
// build/.
func XcodeListBuiltArtifacts(w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintln(w, "Built artifacts:")

	found := false

	walkErr := filepath.WalkDir("build", func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}

			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".ipa" || ext == ".xcarchive" {
			_, _ = fmt.Fprintln(w, path)

			found = true
		}

		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	if !found {
		_, _ = fmt.Fprintln(w, "No artifacts found")
	}

	return nil
}
