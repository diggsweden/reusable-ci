// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// TestLDFlags_EveryBuildInjectsTheVersionVariables checks the -X targets each
// build recipe passes agree with the variables main.go declares.
//
// The linker ignores -X for a symbol that does not exist, so renaming
// main.date, or a typo in one recipe, still builds and ships a binary whose
// --version reports "unknown". The smoke helper finds that at run time for
// its own flags; the release and local recipes are only ever read here.
func TestLDFlags_EveryBuildInjectsTheVersionVariables(t *testing.T) {
	t.Parallel()

	// Taking the addresses fails to compile if a variable is renamed or
	// stops being a string, which is what -X needs.
	declared := map[string]*string{"main.version": &version, "main.commit": &commit, "main.date": &date}
	want := slices.Sorted(maps.Keys(declared))

	target := regexp.MustCompile(`-X ([^=\s]+)=`)

	for _, file := range []string{
		filepath.Join("..", "..", ".goreleaser.yml"),
		filepath.Join("..", "..", "justfile"),
		"smoke_test.go",
	} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		// Counted rather than collected, so one of the justfile's two build
		// recipes dropping a flag the other still passes is a mismatch too.
		seen := map[string]int{}
		for _, match := range target.FindAllSubmatch(body, -1) {
			seen[string(match[1])]++
		}

		if got := slices.Sorted(maps.Keys(seen)); !slices.Equal(got, want) {
			t.Errorf("%s injects %v, want %v", file, got, want)
		}

		if counts := slices.Compact(slices.Sorted(maps.Values(seen))); len(counts) != 1 {
			t.Errorf("%s injects the variables unevenly: %v", file, seen)
		}
	}
}
