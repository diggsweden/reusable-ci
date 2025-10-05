// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// jsonShape and shapeDiff carry no build tag on purpose.
//
// They are pure functions over decoded JSON and the only thing standing between
// "two forges returned different reports" and a green run, but they used to live
// inside the //go:build live file that calls them — so nothing exercised them
// offline, and the shape they compute drifted from the shape the comparison
// needs without anything noticing.
//
// jsonShape records a TYPE at each path, not just the path. Paths alone answer
// "are the same keys present", which is the smaller half of the question. A
// field that changed from a string to a number, an array that arrived empty, an
// array of scalars that became an array of objects: every one of those is the
// same set of paths and a different report, and a consumer parsing it breaks on
// all three.

// jsonShape flattens a decoded document to its sorted set of typed key paths,
// so two reports are compared on structure AND type.
//
// Array elements collapse to a single "[]" step — a consumer cares that every
// element has the same fields, not how many a particular run produced — but the
// array itself is recorded, so an empty array is distinguishable from an absent
// one and from a scalar.
func jsonShape(value any, prefix string) []string {
	var paths []string

	switch typed := value.(type) {
	case map[string]any:
		if prefix != "" {
			paths = append(paths, prefix+":object")
		}

		for _, key := range slices.Sorted(maps.Keys(typed)) {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}

			paths = append(paths, jsonShape(typed[key], path)...)
		}
	case []any:
		paths = append(paths, prefix+":array")
		for _, element := range typed {
			paths = append(paths, jsonShape(element, prefix+"[]")...)
		}
	default:
		paths = append(paths, prefix+":"+jsonScalarKind(value))
	}

	slices.Sort(paths)

	return slices.Compact(paths)
}

// jsonScalarKind names a decoded JSON scalar. json.Number is recognised so a
// decoder configured with UseNumber reports the same kind as one without.
func jsonScalarKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64, json.Number:
		return "number"
	case string:
		return "string"
	default:
		return fmt.Sprintf("%T", value)
	}
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

func TestJSONShape_DistinguishesTypeAsWellAsPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		left, right string
		same        bool
		why         string
	}{
		{
			name: "identical documents", same: true,
			left:  `{"a":"x","b":1}`,
			right: `{"a":"y","b":2}`,
			why:   "values differ, shape does not; that is the whole point of comparing shape",
		},
		{
			name:  "a field changed type",
			left:  `{"count":"3"}`,
			right: `{"count":3}`,
			why:   "PATHS ALONE MISSED THIS. A consumer that parses count as a string breaks on the number",
		},
		{
			name:  "a scalar array became empty",
			left:  `{"tags":["a","b"]}`,
			right: `{"tags":[]}`,
			why:   "PATHS ALONE MISSED THIS: scalars contributed no paths, so both were just `tags`",
		},
		{
			name:  "a scalar array became a scalar",
			left:  `{"tags":["a"]}`,
			right: `{"tags":"a"}`,
			why:   "PATHS ALONE MISSED THIS",
		},
		{
			name:  "an array of scalars became an array of objects",
			left:  `{"items":["a"]}`,
			right: `{"items":[{"name":"a"}]}`,
			why:   "the second needs a different parser",
		},
		{
			name:  "a null where a string was",
			left:  `{"note":"x"}`,
			right: `{"note":null}`,
			why:   "PATHS ALONE MISSED THIS, and null is exactly what a consumer forgets to handle",
		},
		{
			name:  "an object became an empty object",
			left:  `{"meta":{"a":1}}`,
			right: `{"meta":{}}`,
			why:   "the key is gone; paths caught this one already",
		},
		{
			name:  "a missing field",
			left:  `{"a":1,"b":2}`,
			right: `{"a":1}`,
			why:   "the original purpose, still working",
		},
		{
			name: "heterogeneous object arrays union their fields", same: false,
			left:  `{"items":[{"a":1},{"b":2}]}`,
			right: `{"items":[{"a":1}]}`,
			why:   "elements union, so a report whose second element carries an extra field differs",
		},
		{
			name: "element order does not matter", same: true,
			left:  `{"items":[{"a":1},{"b":"x"}]}`,
			right: `{"items":[{"b":"y"},{"a":2}]}`,
			why:   "a union is a set; two runs may order elements differently",
		},
		{
			name: "element count does not matter", same: true,
			left:  `{"items":[{"a":1}]}`,
			right: `{"items":[{"a":1},{"a":2}]}`,
			why:   "how many a particular run produced is not a contract",
		},
		{
			name: "numbers of different magnitude are one kind", same: true,
			left:  `{"n":1}`,
			right: `{"n":1.5}`,
			why:   "JSON has one number type; splitting it would report noise",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			diff := shapeDiff(decodedShape(t, tc.left), decodedShape(t, tc.right))
			if (diff == "") != tc.same {
				t.Errorf("agreed=%v, want %v: %s\nleft:  %s\nright: %s\ndiff:\n%s",
					diff == "", tc.same, tc.why, tc.left, tc.right, diff)
			}
		})
	}
}

func decodedShape(t *testing.T, body string) []string {
	t.Helper()

	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("parse %s: %v", body, err)
	}

	shape := jsonShape(decoded, "")
	if len(shape) == 0 {
		t.Fatalf("%s produced an empty shape; a comparison between two of these proves nothing", body)
	}

	return shape
}
