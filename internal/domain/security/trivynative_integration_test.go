//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/stretchr/testify/require"
)

// This is the evidence the synthetic fixtures could not give: output from the
// Trivy binary this repository pins, parsed by the code that reads Trivy in
// production.
//
// The entry that asked for it assumed a real scan meant a vulnerability
// database download, and so belonged to a separately authorized tier. It does
// not. `trivy fs --skip-db-update --offline-scan` produces a complete native
// envelope against an owned temporary directory and contacts nothing. What that
// cannot produce is a finding, so the vulnerability-bearing shape of Results is
// still checked against constructed documents — and that limit is now the
// narrow, true one rather than "no native evidence at all".
//
// The envelope it does produce is the interesting case anyway. A clean scan
// emits NO Results key: not an empty array, absent. ParseTrivyReport has a rule
// for exactly that — a report is acceptable with a Results array OR a nonblank
// ArtifactName — and until now nothing confirmed real Trivy takes the second
// branch. If it did not, every clean scan would be rejected as malformed.

func TestNativeTrivyEnvelope_ParsesAndCarriesWhatTheCodeReads(t *testing.T) {
	report := runNativeTrivy(t)

	parsed, err := security.ParseTrivyReport(report)
	require.NoError(t, err, "the code that reads Trivy in production rejected real Trivy output:\n%s", report)
	require.NotNil(t, parsed)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(report, &raw))

	// The branch this exercises, stated so a future reader knows why a scan
	// with no findings is the useful case rather than a trivial one.
	_, hasResults := raw["Results"]
	require.False(t, hasResults,
		"a clean scan now emits Results; if that is real Trivy behaviour the ArtifactName branch in "+
			"ParseTrivyReport is no longer what keeps clean scans from being rejected, and this test should "+
			"move to asserting the array instead")
	require.NotEmpty(t, strings.TrimSpace(parsed.ArtifactName),
		"neither Results nor ArtifactName is present, so ParseTrivyReport accepted something it documents as malformed")

	// The version the envelope inventory was verified against must be the
	// version that produced this. Otherwise the two agree only by luck.
	version, _ := raw["Trivy"].(map[string]any)
	require.NotNil(t, version, "the native envelope no longer carries a Trivy version block")
	require.Equal(t, verifiedTrivyVersion, version["Version"],
		"the installed Trivy is not the version consumedTrivyFields was checked against")
}

// An empty report and one with an unknown-only object must still be refused,
// checked here against the real envelope's own shape rather than a guess at it.
func TestNativeTrivyEnvelope_RefusalsHoldAgainstTheRealShape(t *testing.T) {
	report := runNativeTrivy(t)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(report, &raw))

	// Strip exactly the field the acceptance rule depends on. Everything else
	// real Trivy emitted stays, so this is the genuine envelope minus one key
	// rather than a hand-built subset.
	delete(raw, "ArtifactName")

	stripped, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = security.ParseTrivyReport(stripped)
	require.Error(t, err,
		"a real envelope with neither Results nor ArtifactName was accepted; a scan that produced nothing "+
			"would read as a clean image")
}

// runNativeTrivy scans an owned temporary directory with database updates and
// remote lookups disabled, so the run is entirely local.
func runNativeTrivy(t *testing.T) []byte {
	t.Helper()

	binary, err := exec.LookPath("trivy")
	if err != nil {
		t.Skipf("trivy is not on PATH; run `mise install` for the pinned %s", verifiedTrivyVersion)
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"fixture","version":"1.0.0","dependencies":{}}`), 0o600))

	cmd := exec.CommandContext(context.Background(), binary,
		"fs", "--format", "json", "--skip-db-update", "--skip-java-db-update", "--offline-scan", "--quiet", dir)
	cmd.Env = append(os.Environ(), "TRIVY_DISABLE_VEX_NOTICE=1")

	out, err := cmd.Output()
	require.NoErrorf(t, err, "trivy fs failed; this test needs no network but does need the binary: %v", err)
	require.NotEmpty(t, out, "trivy produced no output")

	return out
}
