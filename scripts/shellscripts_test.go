// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package scripts

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNoUnsafeDashLeadingPrintfStrings(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`printf\s+"-[^-]`)

	var findings []string

	if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || filepath.Ext(path) != ".sh" {
			return nil
		}

		file, err := os.Open(path) //nolint:gosec // path comes from filepath.WalkDir inside the repo.
		if err != nil {
			return err
		}

		defer func() { _ = file.Close() }()

		scanner := bufio.NewScanner(file)

		lineNo := 0
		for scanner.Scan() {
			lineNo++

			line := scanner.Text()
			if pattern.MatchString(line) && !strings.Contains(line, `printf -- `) {
				findings = append(findings, fmt.Sprintf("%s:%d:%s", path, lineNo, line))
			}
		}

		return scanner.Err()
	}); err != nil {
		t.Fatal(err)
	}

	if len(findings) > 0 {
		t.Fatalf("found potentially unsafe printf statements:\n%s", strings.Join(findings, "\n"))
	}
}
