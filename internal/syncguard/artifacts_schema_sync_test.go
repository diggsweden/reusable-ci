// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
)

// TestArtifactsSchemaInSync asserts that .reusable-ci/artifacts.schema.json
// on disk matches what cmd/gen-artifacts-schema would produce right now.
// The JSON schema's enums and derived patterns are rendered from the Go
// schema declarations (config.ValidProjectTypes, ValidPublishTargets,
// release.ValidSignMethods, …), so adding a project type or sign method
// in Go without regenerating the published schema fails here instead of
// drifting silently.
//
// On failure, run `just gen-artifacts-schema` to refresh the file.
func TestArtifactsSchemaInSync(t *testing.T) {
	t.Parallel()

	want := config.RenderArtifactsJSONSchema()
	require.NotContains(t, want, "{{",
		"rendered schema still contains a {{placeholder}}; template and renderer drifted apart")
	require.Truef(t, json.Valid([]byte(want)), "rendered schema is not valid JSON")

	path := filepath.Join(reporoot.Path(t), ".reusable-ci", "artifacts.schema.json")
	got, err := os.ReadFile(path) //nolint:gosec // test reads repo-local schema file.
	require.NoErrorf(t, err, "read %s", path)

	require.Equalf(t, want, string(got),
		".reusable-ci/artifacts.schema.json is out of sync with the Go schema declarations; "+
			"run `just gen-artifacts-schema` to refresh")

	// The enums must actually be rendered, not hand-written back into
	// the template — spot-check one value per injected set.
	for _, fragment := range []string{`"maven"`, `"maven-central"`, `"sigstore"`, `"ssh"`, `"application"`} {
		require.Containsf(t, want, fragment, "rendered schema lost the %s enum value", fragment)
	}

	require.NotContains(t, want, "PROJECT_TYPES", "placeholder name leaked into output")
}
