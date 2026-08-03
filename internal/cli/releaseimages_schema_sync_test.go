// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// TestReleaseImagesSchemaInSync asserts that docs/schemas/release-images.schema.json
// on disk matches what cmd/gen-release-images-schema would produce right now.
// The schema's patterns and enums are rendered from the SAME regexes and
// constants the ledger validator enforces (imageRefRE, tagRefRE, sbomRE,
// container.DigestPattern, the ImageKind set), so changing a ledger rule in
// Go without regenerating the published schema fails here instead of
// drifting silently — the failure mode the previous hand-maintained copy in
// forgejo-ci had (it required fields the validator treats as optional and
// rejected fields the Entry accepts).
//
// On failure, run `just gen-release-images-schema`.
func TestReleaseImagesSchemaInSync(t *testing.T) {
	t.Parallel()

	want := imageledger.RenderReleaseImagesJSONSchema()
	require.NotContains(t, want, "{{",
		"rendered schema still contains a {{placeholder}}; template and renderer drifted apart")
	require.Truef(t, json.Valid([]byte(want)), "rendered schema is not valid JSON")

	path := filepath.Join(repoRoot(t), "docs", "schemas", "release-images.schema.json")
	got, err := os.ReadFile(path) //nolint:gosec // test reads repo-local schema file.
	require.NoErrorf(t, err, "read %s", path)

	require.Equalf(t, want, string(got),
		"docs/schemas/release-images.schema.json is out of sync with the ledger validator; "+
			"run `just gen-release-images-schema`")

	// The patterns must actually be rendered from the validator, not
	// hand-written back into the template — spot-check one injected value
	// per set.
	for _, fragment := range []string{`"release", "base"`, `sha256:[0-9a-f]{64}`, `cyclonedx`} {
		require.Containsf(t, want, fragment, "rendered schema lost the %s fragment", fragment)
	}
}
