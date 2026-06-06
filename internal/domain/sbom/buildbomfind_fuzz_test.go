// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

// FuzzFindBuildBOM exercises the include/exclude glob matcher. The
// patterns ultimately compile to regex; a bad pattern shouldn't take
// down the binary — at worst no candidate matches.
func FuzzFindBuildBOM(f *testing.F) {
	seeds := []struct{ files, includes, excludes string }{
		{"target/bom.json", "*/target/bom.json", ""},
		{"build/reports/bom.json\nbuild/reports/cyclonedx/bom.json", "*/build/reports/bom.json,*/build/reports/cyclonedx/bom.json", ""},
		{"node_modules/lib/bom.json\napp/bom.json", "*/bom.json", "*/node_modules/*"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"", "*/bom.json", ""},
		{strings.Repeat("a/", 100) + "bom.json", "*/bom.json", ""},
	}
	for _, s := range seeds {
		f.Add(s.files, s.includes, s.excludes)
	}

	f.Fuzz(func(t *testing.T, files, includes, excludes string) {
		in := sbom.FindBuildBOMInput{
			Files:    strings.Split(files, "\n"),
			Includes: strings.Split(includes, ","),
			Excludes: strings.Split(excludes, ","),
		}
		_ = sbom.FindBuildBOM(in)
	})
}
