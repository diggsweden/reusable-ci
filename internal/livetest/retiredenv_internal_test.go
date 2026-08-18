// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestLiveConsumer_NoRetiredAmbientContractReads(t *testing.T) {
	t.Parallel()

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`LAB_TARGETS(?:[^_A-Z]|$)`),
		regexp.MustCompile(`LAB_CA_FILE`),
		regexp.MustCompile(`LAB_FULCIO_URL`),
		regexp.MustCompile(`LAB_FULCIO_ISSUERS`),
	}

	paths := []string{
		"capability.go", "environment.go", "livetest.go", "workflow.go",
		filepath.Join("..", "..", "scripts", "ci", "validate-live-inputs.sh"),
		filepath.Join("..", "..", "scripts", "ci", "run-live-tests.sh"),
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		for _, pattern := range patterns {
			if pattern.Match(body) {
				t.Errorf("%s still contains retired ambient input matching %s", path, pattern)
			}
		}
	}
}
