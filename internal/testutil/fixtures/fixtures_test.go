// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fixtures_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fixtures"
)

func TestRead_FoundFile(t *testing.T) {
	got := fixtures.Read(t, "artifacts/empty.yml")
	if !strings.Contains(string(got), "schemaVersion: 1") {
		t.Errorf("unexpected fixture content: %q", got)
	}
}

func TestArtifactsYAMLEmpty_Matches(t *testing.T) {
	got := fixtures.ArtifactsYAMLEmpty()

	want := fixtures.Read(t, "artifacts/empty.yml")
	if string(got) != string(want) {
		t.Errorf("getter and Read() returned different bytes")
	}
}

func TestChangelogMinimal_HasHeader(t *testing.T) {
	got := fixtures.ChangelogMinimal()
	if !strings.Contains(string(got), "# Changelog") {
		t.Errorf("ChangelogMinimal missing # Changelog header")
	}
}

func TestList_FindsFiles(t *testing.T) {
	files := fixtures.List(t, "artifacts")
	if len(files) == 0 {
		t.Errorf("List(artifacts) returned no entries")
	}

	for _, f := range files {
		if strings.HasPrefix(f, "/") {
			t.Errorf("List returned absolute path: %q", f)
		}
	}
}

func TestMustRead_PanicsOnMiss(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustRead did not panic on missing file")
		}
	}()

	_ = fixtures.MustRead("does/not/exist.yml")
}
