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
// build/. Mirrors scripts/apple/list-built-artifacts.sh.
func XcodeListBuiltArtifacts(stdout io.Writer) error {
	fmt.Fprintln(stdout, "Built artifacts:")
	any := false
	walkErr := filepath.WalkDir("build", func(path string, d fs.DirEntry, err error) error {
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
			fmt.Fprintln(stdout, path)
			any = true
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if !any {
		fmt.Fprintln(stdout, "No artifacts found")
	}
	return nil
}
