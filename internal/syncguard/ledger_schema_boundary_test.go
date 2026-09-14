// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"bytes"
	"encoding/json"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestLedgerSchemaBoundary_CompilesAndMatchesSharedRuntimeCases(t *testing.T) {
	t.Parallel()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(reporoot.ReadFile(t, "docs/schemas/release-images.schema.json")))
	require.NoError(t, err)

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{}) // Only registered resources and built-in metaschemas; no file or network resolution.
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("ledger.json", document))
	schema, err := compiler.Compile("ledger.json")
	require.NoError(t, err)

	digest := "sha256:" + strings.Repeat("a", 64)
	base := map[string]any{"ref": "registry.example/owner/app@" + digest, "digest": digest, "final_tag": "registry.example/owner/app:v1.2.3"}

	for _, field := range []string{"valid", "base", "extra", "ref", "digest", "final_tag", "image_kind", "sbom", "sbom_sha256", "provenance", "missing"} {
		entry := map[string]any{}
		for key, value := range base {
			entry[key] = value
		}

		switch field {
		case "valid":
		case "base":
			entry["image_kind"] = "base"
			entry["final_tag"] = "registry.example/owner/app:base-build"
		case "extra":
			entry["unknown_field"] = true
		case "missing":
			delete(entry, "digest")
		case "provenance":
			entry[field] = "wrong-type"
		default:
			entry[field] = "invalid"
		}

		body, err := json.Marshal([]any{entry})
		require.NoError(t, err)
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		require.NoError(t, err)

		schemaErr := schema.Validate(instance)

		entries, parseErr := imageledger.Parse(body)
		if parseErr == nil {
			parseErr = imageledger.ValidateAll(entries, "v1.2.3")
		}

		switch field {
		case "valid", "base":
			require.NoError(t, schemaErr)
			require.NoError(t, parseErr)
		default:
			require.Error(t, schemaErr, field)
			require.Error(t, parseErr, field)
		}
	}
}
