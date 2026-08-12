// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-UX-3: --json emits the same shape on every forge.
//
// Machine output is a contract with somebody else's script, and the whole point
// of a multi-forge tool is that the script does not have to care which forge it
// ran against. A field that appears only on GitHub, or a capability key that is
// spelled one way on one adapter, forces every consumer into per-forge handling
// — which is the thing this product exists to remove.
//
// Values are expected to differ: that is the forge showing through, and the test
// requires it, because a shape comparison passes trivially if the command
// secretly reported the same forge twice.
//
// `doctor --json` is the surface under test because it is the one that renders
// the most forge-derived state — the detected platform, the runner dialect, and
// the whole capability model — through one schema.

import (
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestJSON_DoctorReport_HasTheSameShapeOnEveryForge(t *testing.T) {
	shapes := map[provider.ForgeAPI][]string{}
	forgeAPI := map[provider.ForgeAPI]string{}

	forges := forgesClaiming(t, alwaysValidatesTokens, "a JSON report")

	for _, forge := range forges {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "json-shape")

			run := livetest.CLI(t, target, repo, "doctor", "--json")

			// doctor reports problems by exit code; a non-zero exit still has to
			// produce a parsable report, and that is part of the contract.
			var report map[string]any
			if err := json.Unmarshal([]byte(run.Stdout), &report); err != nil {
				t.Fatalf("%s: doctor --json did not emit parsable JSON on stdout (exit %d): %v\nstdout: %s\nstderr: %s",
					forge, run.ExitCode, err, run.Stdout, run.Stderr)
			}

			shapes[forge] = jsonShape(report, "")

			if env, ok := report["environment"].(map[string]any); ok {
				forgeAPI[forge], _ = env["forge_api"].(string)
			}
		})
	}

	if len(shapes) < 2 {
		t.Fatalf("only %d forge(s) produced a report; a parity comparison needs two", len(shapes))
	}

	// Non-vacuity: the reports must actually come from different forges. Without
	// this, a doctor that hard-coded one platform would pass the shape check
	// perfectly.
	for _, forge := range forges {
		if got := forgeAPI[forge]; got != string(forge) {
			t.Errorf("%s reported forge_api %q, so the reports are not from the forges they claim",
				forge, got)
		}
	}

	// The shape must be a real schema, not a nearly-empty document that two
	// forges agree on by accident. These are the deepest paths the report has, so
	// requiring them proves the flattening descended and the comparison below is
	// over something worth comparing.
	for _, required := range []string{
		"environment.forge_api",
		"environment.runner",
		"environment.capabilities.container_tag_deletion",
		"checks[].name",
		"checks[].severity",
	} {
		for _, forge := range forges {
			if !slices.Contains(shapes[forge], required) {
				t.Errorf("%s: doctor --json has no %s; the shape comparison would prove little",
					forge, required)
			}
		}
	}

	reference := forges[0]
	for _, forge := range forges[1:] {
		if diff := shapeDiff(shapes[reference], shapes[forge]); diff != "" {
			t.Errorf("doctor --json has a different shape on %s than on %s, so a consumer must special-case per forge:\n%s",
				forge, reference, diff)
		}
	}
}

// jsonShape flattens a decoded document to its sorted set of key paths, so two
// reports are compared on structure alone. Array elements collapse to a single
// "[]" step: a consumer cares that every element has the same fields, not how
// many a particular run produced.
func jsonShape(value any, prefix string) []string {
	var paths []string

	switch typed := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}

			paths = append(paths, path)
			paths = append(paths, jsonShape(typed[key], path)...)
		}
	case []any:
		for _, element := range typed {
			paths = append(paths, jsonShape(element, prefix+"[]")...)
		}
	}

	sort.Strings(paths)

	return slices.Compact(paths)
}

// shapeDiff reports the fields present on one forge and not the other, in both
// directions, or "" when the shapes agree.
func shapeDiff(reference, other []string) string {
	var lines []string

	for _, path := range reference {
		if !slices.Contains(other, path) {
			lines = append(lines, "  missing: "+path)
		}
	}

	for _, path := range other {
		if !slices.Contains(reference, path) {
			lines = append(lines, "  extra:   "+path)
		}
	}

	return strings.Join(lines, "\n")
}
