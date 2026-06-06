// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package projecttype_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func FuzzDetectFromEntries(f *testing.F) {
	seeds := []string{
		"",
		"pom.xml",
		"package.json", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"build.gradle", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"build.gradle.kts",
		"go.mod",
		"Cargo.toml",
		"pyproject.toml",
		"requirements.txt",
		"setup.py",
		"README.md\nLICENSE",
		"pom.xml\npackage.json",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		entries := []string{}
		if raw != "" {
			entries = strings.Split(raw, "\n")
		}

		got := projecttype.DetectFromEntries(entries)
		switch got {
		case projecttype.Maven, projecttype.NPM, projecttype.Gradle, projecttype.Go, projecttype.Cargo, projecttype.Python, projecttype.Unknown:
		default:
			t.Fatalf("unexpected project type %q for %v", got, entries)
		}
	})
}
