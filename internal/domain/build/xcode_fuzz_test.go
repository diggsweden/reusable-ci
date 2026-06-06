// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

// FuzzParseXcodeVersionFromPbxproj exercises the project.pbxproj
// regex-based reader. The format is undocumented and arbitrary garbage
// is plausible (the file is normally machine-generated but humans edit
// it). The fuzzer's job is to find inputs that panic — the bash
// equivalent returns empty strings for malformed values too, so we
// only assert "no panic, output is a stable XcodeVersionInfo pair".
func FuzzParseXcodeVersionFromPbxproj(f *testing.F) {
	seeds := []string{
		``,
		`MARKETING_VERSION = 1.2.3;`,
		`CURRENT_PROJECT_VERSION = 42;`,
		`MARKETING_VERSION = "1.2.3-beta";`,
		"MARKETING_VERSION = 1.0;\nCURRENT_PROJECT_VERSION = 100;",
		strings.Repeat("MARKETING_VERSION = 1.0;\n", 100),
		"MARKETING_VERSION = " + strings.Repeat("a", 1000) + ";",
		`weird = "MARKETING_VERSION = inside-a-string";`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, body string) {
		// The contract is "never panics". An input like
		// `MARKETING_VERSION = ;` legitimately produces an empty value
		// — mirrors the bash `awk -F' = '` behaviour on empty fields.
		_ = build.ParseXcodeVersionFromPbxproj(body)
	})
}
