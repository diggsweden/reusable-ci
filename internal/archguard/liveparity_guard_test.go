// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// forgeAwareMarkers are the imports that make a command's behaviour depend on
// which forge it is pointed at: a provider adapter, or the run context it reads
// from the surrounding job. A command importing neither behaves the same
// everywhere, so it has no parity to verify -- `build` and `sbom` take bytes and
// produce bytes.
func forgeAwareMarkers() []string {
	return []string{internalPrefix + "domain/provider", internalPrefix + "runcontext"}
}

// TestForgeAwareCommandsHaveALiveScenario fails when a command group's
// behaviour varies by forge and no conformance scenario invokes it.
//
// The live tier is the only layer that can catch a forge disagreeing with the
// adapter written from the same assumptions as its fake, so a forge-aware
// command with no scenario is untested in the one place that counts. Six of the
// sixteen command groups have no scenario today and all six are correctly
// exempt -- but that was established by reading the imports once, by hand, and
// nothing would notice `build` growing a provider dependency later. This is
// that reading, kept honest.
//
// One direction only. A scenario covering a forge-agnostic command is not a
// defect: `publish` and `doctor` are exercised live because it is the cheapest
// place to run the real binary, not because they vary.
func TestForgeAwareCommandsHaveALiveScenario(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	scenarios := readScenarios(t, filepath.Join(root, "internal", "livetest", "conformance"))

	commandsDir := filepath.Join(root, "internal", "cli", "commands")

	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		t.Fatalf("read command groups: %v", err)
	}

	var (
		uncovered []string
		checked   int
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		marker, aware := forgeAwareness(t, filepath.Join(commandsDir, entry.Name()))
		if !aware {
			continue
		}

		checked++

		// The scenarios drive the built binary, so a covered command appears as
		// its own name in an argument list. Matching the quoted name rather than
		// parsing the call keeps this from having an opinion about how a
		// scenario spells its invocation.
		if !strings.Contains(scenarios, `"`+entry.Name()+`"`) {
			uncovered = append(uncovered,
				entry.Name()+"\n    imports "+marker+", so its behaviour varies by forge\n"+
					`    but no scenario under internal/livetest/conformance/ invokes "`+entry.Name()+`"`)
		}
	}

	// A rule that silently matches nothing has stopped being a rule. This one
	// would, if the command tree moved.
	if checked == 0 {
		t.Fatalf("no forge-aware command groups found under %s; the guard is looking in the wrong place",
			commandsDir)
	}

	if len(uncovered) > 0 {
		slices.Sort(uncovered)
		t.Fatalf("forge-aware commands with no live scenario:\n\n%s\n\n"+
			"Add a scenario under internal/livetest/conformance/ that iterates\n"+
			"livetest.LiveForges(), so one body runs against every forge. If the\n"+
			"command does not actually vary by forge, drop the import instead.",
			strings.Join(uncovered, "\n\n"))
	}
}

// forgeAwareness reports the first marker import found in a command group's
// non-test sources, which is also the explanation the failure needs.
func forgeAwareness(t *testing.T, dir string) (string, bool) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		source, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir, name), err)
		}

		for _, marker := range forgeAwareMarkers() {
			if strings.Contains(string(source), `"`+marker+`"`) {
				return marker, true
			}
		}
	}

	return "", false
}

// readScenarios concatenates the conformance sources, so the coverage question
// is asked of the whole suite rather than file by file.
func readScenarios(t *testing.T, dir string) string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}

	if len(paths) == 0 {
		t.Fatalf("no scenarios found in %s; the guard is looking in the wrong place", dir)
	}

	var all strings.Builder

	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		all.Write(source)
	}

	return all.String()
}
