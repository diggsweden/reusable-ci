// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemaPath resolves the schema relative to the repo root. The test
// file lives several directories deep, so we walk up until we find
// either the schema file or the repo's go.mod.
func schemaPath(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".reusable-ci", "artifacts.schema.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	t.Fatal("could not find .reusable-ci/artifacts.schema.json walking up from test cwd")

	return ""
}

// loadSchema compiles the project schema with the draft pinned to
// 2020-12 — the same draft the schema file declares. Pinning is
// explicit per the v6 library's guidance: the default-latest behaviour
// is documented as subject to change across library releases.
func loadSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	body, err := os.ReadFile(schemaPath(t))
	if err != nil {
		t.Fatal(err)
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse schema as JSON: %v", err)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)

	if err = c.AddResource("artifacts.schema.json", doc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}

	sch, err := c.Compile("artifacts.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	return sch
}

// yamlToInstance round-trips a YAML body into the same untyped
// any-shape the validator expects (parsed from JSON via
// jsonschema.UnmarshalJSON, which preserves number precision via
// json.Number).
func yamlToInstance(t *testing.T, body []byte) any {
	t.Helper()

	var raw any
	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}

	jsonBody, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal as json: %v", err)
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("parse instance as JSON: %v", err)
	}

	return inst
}

// TestSchema_ValidatesEveryExample sweeps the examples/ directory and
// asserts every shipped artifacts.yml passes the JSON Schema. Catches
// schema drift in either direction — schema rejecting valid code, or
// example using a field the schema doesn't know.
func TestSchema_ValidatesEveryExample(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)
	root := repoRoot(t)

	// Walked, not globbed at a fixed depth: examples/ is grouped
	// (examples/signing/…, examples/gitlab/…), and a glob of examples/*/ drops
	// the nested ones silently -- the sweep keeps passing while validating less
	// than it claims. A new example is picked up with nothing to update here.
	var examples []string

	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !d.IsDir() && d.Name() == "artifacts.yml" {
			examples = append(examples, path)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(examples) == 0 {
		t.Fatal("no example artifacts.yml files found; did the layout change?")
	}

	for _, path := range examples {
		// Named by path relative to examples/, so a grouped example reads as
		// "signing/openbao-kms" rather than colliding on its bare directory name.
		name, relErr := filepath.Rel(filepath.Join(root, "examples"), filepath.Dir(path))
		if relErr != nil {
			t.Fatal(relErr)
		}

		t.Run(filepath.ToSlash(name), func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if err := schema.Validate(yamlToInstance(t, body)); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		})
	}
}

// TestSchema_RejectsUnknownTopLevelField guards against the schema
// quietly tolerating typos.
func TestSchema_RejectsUnknownTopLevelField(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)

	bad := []byte(`{
  "artifacts": [{"name": "x", "project-type": "maven"}],
  "WHAT_IS_THIS": "should fail"
}`)

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}

	if err := schema.Validate(inst); err == nil {
		t.Error("schema must reject unknown top-level fields")
	}
}

func TestSchema_RejectsUnknownArtifactField(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)

	bad := []byte(`{
  "artifacts": [{"name": "x", "project-type": "maven", "OOPSIE": true}]
}`)

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}

	verr := schema.Validate(inst)
	if verr == nil {
		t.Fatal("schema must reject unknown artifact fields")
	}

	// santhosh-tekuri returns a wrapped *ValidationError; the underlying
	// reason for an additionalProperties rejection cites the offending
	// key in either the error message or the structured cause tree.
	msg := verr.Error()

	var vErr *jsonschema.ValidationError
	if errors.As(verr, &vErr) {
		msg = vErr.Error()
	}

	if !strings.Contains(msg, "OOPSIE") && !strings.Contains(msg, "additionalProperties") {
		t.Errorf("error should cite the offending key; got:\n%s", msg)
	}
}

func TestSchema_RejectsInvalidProjectType(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)

	bad := []byte(`{
  "artifacts": [{"name": "x", "project-type": "made-up-language"}]
}`)

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}

	if err := schema.Validate(inst); err == nil {
		t.Error("schema must reject unknown project-type values")
	}
}

func TestSchema_SignBlock_AcceptsHappyPaths(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)

	cases := []struct {
		name string
		body string
	}{
		{"omitted entirely", `{"artifacts":[{"name":"x","project-type":"maven"}]}`},
		{"method gpg", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"gpg"}}`},
		{"method sigstore minimal", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"sigstore"}}`},
		{"method sigstore with issuer", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"sigstore","oidc-issuer":"https://token.actions.githubusercontent.com"}}`},
		{"method kms hashivault", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"hashivault://transit/keys/release"}}`},
		{"method kms awskms", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"awskms:///alias/release-signing"}}`},
		{"method kms file local-dev", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"file:./signing.key"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(tc.body)))
			if err != nil {
				t.Fatal(err)
			}

			if err := schema.Validate(inst); err != nil {
				t.Errorf("%v", err)
			}
		})
	}
}

func TestSchema_SignBlock_RejectsBadConfigurations(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t)

	cases := []struct {
		name string
		body string
	}{
		{"unknown method", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"openssl"}}`},
		{"unknown key", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"hashivault://k","TYPO":true}}`},
		{"http oidc issuer", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"sigstore","oidc-issuer":"http://example.com"}}`},
		{"local key without file prefix", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"./local.key"}}`},
		{"arbitrary scheme", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"http://attacker/key"}}`},
		{"absolute path", `{"artifacts":[{"name":"x","project-type":"maven"}],"sign":{"method":"kms","key":"/etc/secrets/key"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(tc.body)))
			if err != nil {
				t.Fatal(err)
			}

			if err := schema.Validate(inst); err == nil {
				t.Errorf("schema must reject %s; body=%s", tc.name, tc.body)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// .reusable-ci/ lives at the repo root; resolve once for re-use.
	return filepath.Dir(filepath.Dir(schemaPath(t)))
}
