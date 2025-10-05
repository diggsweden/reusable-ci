// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package projecttype_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// knownManifests lists every basename DetectFromEntries recognises. It is the
// set of names, not their priority, so the properties below can check the
// detector without re-implementing its ordering.
//
//nolint:gochecknoglobals // read-only fixture shared by the fuzz properties.
var knownManifests = []string{
	"pom.xml", "package.json", "build.gradle", "build.gradle.kts", "go.mod",
	"Cargo.toml", "pyproject.toml", "requirements.txt", "setup.py",
}

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

		// The enum check above is satisfied by a detector that always returns
		// Unknown. These properties are not: they tie the answer to what the
		// entries contain, without restating the priority table.

		// Unknown exactly when no recognised manifest is present.
		hasManifest := slices.ContainsFunc(entries, func(e string) bool { return slices.Contains(knownManifests, e) })
		if (got == projecttype.Unknown) == hasManifest {
			t.Fatalf("DetectFromEntries(%q) = %q, but a recognised manifest present = %v", entries, got, hasManifest)
		}

		// pom.xml outranks everything, so its presence decides the answer.
		if slices.Contains(entries, "pom.xml") && got != projecttype.Maven {
			t.Fatalf("DetectFromEntries(%q) = %q with pom.xml present, want maven", entries, got)
		}

		// The answer depends on which names are present, not their order,
		// and an unrelated file cannot change it.
		reversed := slices.Clone(entries)
		slices.Reverse(reversed)

		if again := projecttype.DetectFromEntries(append(reversed, "README.md")); again != got {
			t.Fatalf("reordering %q and adding README.md changed the answer from %q to %q", entries, got, again)
		}
	})
}
