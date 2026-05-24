// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package fixtures bundles checked-in test data accessible from any test
// in the repo. Files live under testdata/ and are exposed via //go:embed
// + named getters so tests don't have to compute paths.
//
// Adding a fixture: drop the file under testdata/, add a getter, add a
// test exercising it.
package fixtures

import (
	"embed"
	"io/fs"
	"path"
	"testing"
)

//go:embed testdata
var embedFS embed.FS

// Read returns the bytes of testdata/<p>, fataling the test on miss.
func Read(t *testing.T, p string) []byte { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	t.Helper()

	full := path.Join("testdata", p)

	data, err := embedFS.ReadFile(full)
	if err != nil {
		t.Fatalf("fixtures.Read(%q): %v", p, err)
	}

	return data
}

// MustRead is the non-test variant for use in *_test.go init / table data.
func MustRead(p string) []byte {
	full := path.Join("testdata", p)

	data, err := embedFS.ReadFile(full)
	if err != nil {
		panic("fixtures.MustRead(" + p + "): " + err.Error())
	}

	return data
}

// List returns relative file paths under testdata/<dir>, fataling on error.
// Useful for table-driven tests that exhaust a fixture directory.
func List(t *testing.T, dir string) []string {
	t.Helper()

	prefix := path.Join("testdata", dir)

	var out []string

	err := fs.WalkDir(embedFS, prefix, func(p string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel := p[len(prefix)+1:]
		out = append(out, rel)

		return nil
	})
	if err != nil {
		t.Fatalf("fixtures.List(%q): %v", dir, err)
	}

	return out
}

// Convenience getters for the canonical fixtures. Each getter is a
// named accessor so callers don't pass fixture paths as strings.

// ArtifactsYAMLEmpty returns a minimal artifacts.yml with only schemaVersion.
func ArtifactsYAMLEmpty() []byte { return MustRead("artifacts/empty.yml") }

// ChangelogMinimal returns a one-entry CHANGELOG.md.
func ChangelogMinimal() []byte { return MustRead("changelog/minimal.md") }
