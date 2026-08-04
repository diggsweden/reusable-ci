// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"bytes"
	"strings"
	"testing"

	appdoctor "github.com/diggsweden/reusable-ci/v3/internal/app/doctor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestFormatEnvironment_GitHubAllCapabilities(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	appdoctor.FormatEnvironment(&buf, appdoctor.Environment{
		Provider: "GitHub",
		ForgeAPI: "github",
		Runner:   "github",
		Capabilities: provider.Capabilities{
			SARIFUpload: true, Attestation: true, ReleaseAssets: true, RunArtifacts: true,
			PublicFulcioTrusted: true, MintsOIDCToken: true,
		},
	})

	out := buf.String()
	if !strings.Contains(out, "GitHub (github)") || !strings.Contains(out, "runner conventions:   github") {
		t.Errorf("missing resolved runtime line:\n%s", out)
	}

	// All capabilities on → no degradation hints printed.
	if strings.Contains(out, "→") {
		t.Errorf("unexpected degradation hint when all capabilities present:\n%s", out)
	}
}

func TestFormatEnvironment_ForgejoDegradesSARIF(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	appdoctor.FormatEnvironment(&buf, appdoctor.Environment{
		Provider:     "Forgejo",
		ForgeAPI:     "forgejo",
		Runner:       "forgejo",
		Capabilities: provider.Capabilities{ReleaseAssets: true},
	})

	out := buf.String()
	if !strings.Contains(out, "Forgejo (forgejo)") {
		t.Errorf("missing forge line:\n%s", out)
	}

	// SARIF unavailable → its degradation hint must be shown.
	if !strings.Contains(out, "step-summary") {
		t.Errorf("expected SARIF degradation hint:\n%s", out)
	}

	// Release-asset upload is available → no hint for it.
	if strings.Contains(out, "release assets cannot be attached") {
		t.Errorf("release-asset hint shown despite capability present:\n%s", out)
	}
}
