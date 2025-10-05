// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
)

// The existing sync guards compare the published schema to what the renderer
// would emit right now. That catches a template edited without regenerating,
// and it is genuinely load-bearing — but both sides of that comparison come
// from the same place. The schema is never compared to the code that actually
// parses the YAML.
//
// So a field added to config.Artifact and not to the template drifts silently
// in the direction adopters feel: the parser accepts their file, their editor
// says the field is not allowed, and nothing here fails. A field removed from
// the Go struct but left in the template drifts the other way — the schema
// promises something no longer read.
//
// This is the independent inventory: field names, requiredness and object
// closedness taken from the Go types by reflection, compared against the
// published schema. Where they differ on purpose the difference is declared
// below with its reason, so "we meant that" is written down rather than
// inferred from a passing test.

// schemaObject is one object in a JSON Schema document, reduced to what this
// comparison is about.
type schemaObject struct {
	Properties           map[string]json.RawMessage `json:"properties"`
	Required             []string                   `json:"required"`
	AdditionalProperties *bool                      `json:"additionalProperties"`
}

// runtimeObject is the same shape derived from a Go struct's yaml tags.
type runtimeObject struct {
	fields   []string
	required []string
}

// declaredSchemaDifference records a field the schema and the runtime
// deliberately disagree about, with the reason. An undeclared difference fails.
type declaredSchemaDifference struct {
	object string
	field  string
	reason string
}

func declaredArtifactsSchemaDifferences() []declaredSchemaDifference {
	return []declaredSchemaDifference{
		{
			object: "$defs/artifact", field: "config",
			reason: "the schema leaves `config` an open object because its shape depends on project-type; " +
				"the Go side has one typed struct per ecosystem behind a custom UnmarshalYAML, and both " +
				"are strict at parse time (KnownFields), so a typo fails with a clear error rather than " +
				"being silently dropped. Constraining it in JSON Schema would need a per-project-type " +
				"conditional the renderer does not emit.",
		},
	}
}

func TestArtifactsSchemaMatchesTheTypesThatParseTheYAML(t *testing.T) {
	t.Parallel()

	document := parseSchemaDocument(t, ".reusable-ci/artifacts.schema.json")

	for _, tc := range []struct {
		object  string
		runtime any
	}{
		{"", config.Config{}},
		{"$defs/artifact", config.Artifact{}},
		{"$defs/container", config.Container{}},
		{"$defs/sign", config.SignConfig{}},
		{"$defs/git-signing", config.GitSigningConfig{}},
	} {
		t.Run(objectLabel(tc.object), func(t *testing.T) {
			t.Parallel()

			compareSchemaToRuntime(t, document, tc.object, "yaml", reflect.TypeOf(tc.runtime), declaredArtifactsSchemaDifferences())
		})
	}
}

func TestReleaseImagesSchemaMatchesTheTypesThatParseTheJSON(t *testing.T) {
	t.Parallel()

	document := parseSchemaDocument(t, "docs/schemas/release-images.schema.json")

	// The ledger is a bare JSON array, so the object under comparison is the
	// array's `items`, and the wire tags are json rather than yaml.
	compareSchemaToRuntime(t, document, "items", "json", reflect.TypeOf(imageledger.Entry{}), nil)
}

// compareSchemaToRuntime is the whole comparison: same field set, same
// requiredness, and a closed object on the schema side because the Go side is
// closed at parse time.
func compareSchemaToRuntime(t *testing.T, document map[string]json.RawMessage, object, tagKey string, runtime reflect.Type, declared []declaredSchemaDifference) {
	t.Helper()

	schema := schemaObjectAt(t, document, object)
	fromGo := runtimeObjectOf(t, runtime, tagKey)

	excused := map[string]string{}

	for _, difference := range declared {
		if difference.object == object {
			excused[difference.field] = difference.reason
		}
	}

	schemaFields := sortedKeys(schema.Properties)

	for _, field := range diff(schemaFields, fromGo.fields) {
		if _, ok := excused[field]; ok {
			continue
		}

		t.Errorf("%s: the schema declares %q but %s has no such yaml field; the schema promises something nothing reads",
			objectLabel(object), field, runtime.Name())
	}

	for _, field := range diff(fromGo.fields, schemaFields) {
		if _, ok := excused[field]; ok {
			continue
		}

		t.Errorf("%s: %s parses %q but the schema does not declare it; an adopter's editor will reject a file the tool accepts",
			objectLabel(object), runtime.Name(), field)
	}

	if got, want := sorted(schema.Required), sorted(fromGo.required); !equal(got, want) {
		t.Errorf("%s: schema requires %v, %s requires %v (a field without `omitempty` is required)",
			objectLabel(object), got, runtime.Name(), want)
	}

	// The Go decoders use KnownFields(true) at every level. A schema that
	// allowed extra properties would tell adopters the opposite.
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Errorf("%s: the schema permits additional properties, but the parser rejects unknown keys; "+
			"an adopter would be told a file is valid that the tool refuses", objectLabel(object))
	}
}

// runtimeObjectOf reads a struct's yaml tags. A field without `omitempty` is
// required, which is the convention this configuration already follows and the
// only signal the tags carry.
func runtimeObjectOf(t *testing.T, typ reflect.Type, tagKey string) runtimeObject {
	t.Helper()

	require.Equal(t, reflect.Struct, typ.Kind(), "inventory is over structs")

	out := runtimeObject{}

	for i := range typ.NumField() {
		tag, ok := typ.Field(i).Tag.Lookup(tagKey)
		if !ok {
			continue
		}

		name, options, _ := strings.Cut(tag, ",")
		if name == "-" || name == "" {
			continue
		}

		out.fields = append(out.fields, name)

		if !strings.Contains(options, "omitempty") {
			out.required = append(out.required, name)
		}
	}

	require.NotEmpty(t, out.fields, "%s exposed no yaml fields; the reflection, not the type, is what was measured", typ.Name())

	return out
}

func schemaObjectAt(t *testing.T, document map[string]json.RawMessage, object string) schemaObject {
	t.Helper()

	body := document["\x00root"]

	switch object {
	case "":
	case "items":
		var ok bool

		body, ok = document["items"]
		require.True(t, ok, "the schema has no items; the comparison below would be against nothing")
	default:
		defs := map[string]json.RawMessage{}
		require.NoError(t, json.Unmarshal(document["$defs"], &defs))

		name := strings.TrimPrefix(object, "$defs/")

		var ok bool

		body, ok = defs[name]
		require.Truef(t, ok, "the schema has no $defs/%s; the comparison below would be against nothing", name)
	}

	var parsed schemaObject
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.NotEmptyf(t, parsed.Properties, "%s declares no properties", objectLabel(object))

	return parsed
}

func parseSchemaDocument(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()

	raw := reporoot.ReadFile(t, path)

	document := map[string]json.RawMessage{}
	require.NoErrorf(t, json.Unmarshal(raw, &document), "parse %s", path)

	document["\x00root"] = raw

	return document
}

func objectLabel(object string) string {
	if object == "" {
		return "(root)"
	}

	return object
}

func diff(from, remove []string) []string {
	drop := map[string]bool{}
	for _, item := range remove {
		drop[item] = true
	}

	var out []string

	for _, item := range from {
		if !drop[item] {
			out = append(out, item)
		}
	}

	return out
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)

	return out
}

func sortedKeys(in map[string]json.RawMessage) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
