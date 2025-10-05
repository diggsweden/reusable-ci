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

func TestFormatEnvironment_CompleteCapabilityMatrix(t *testing.T) {
	t.Parallel()

	const header = "Environment:\n  provider (forge API): Fixture Forge (fixture-api)\n  runner conventions:   fixture-runner\nCapabilities:\n"

	rows := []struct {
		name    string
		only    provider.Capabilities
		on, off string
	}{
		{"SARIF", provider.Capabilities{SARIFUpload: true},
			"  [yes] SARIF / Code Scanning upload\n",
			"  [ no] SARIF / Code Scanning upload\n        \u2192 findings degrade to the step-summary + uploaded artifact\n"},
		{"attestation", provider.Capabilities{Attestation: true},
			"  [yes] SLSA build provenance API\n",
			"  [ no] SLSA build provenance API\n        \u2192 no forge attestation API; `release provenance` still emits a cosign-signed statement\n"},
		{"public-Fulcio", provider.Capabilities{PublicFulcioTrusted: true},
			"  [yes] keyless OIDC signing (public Fulcio)\n",
			"  [ no] keyless OIDC signing (public Fulcio)\n        \u2192 use key-based signing, or pass --oidc-issuer and --fulcio-url for your own CA\n"},
		{"own-Fulcio", provider.Capabilities{MintsOIDCToken: true},
			"  [yes] keyless OIDC signing (own Fulcio)\n",
			"  [ no] keyless OIDC signing (own Fulcio)\n        \u2192 this forge mints no OIDC id-token; use key-based signing\n"},
		{"release-assets", provider.Capabilities{ReleaseAssets: true},
			"  [yes] release asset upload\n",
			"  [ no] release asset upload\n        \u2192 release assets cannot be attached on this forge\n"},
		{"run-artifacts", provider.Capabilities{RunArtifacts: true},
			"  [yes] run-artifact store (intra-run hand-off)\n",
			"  [ no] run-artifact store (intra-run hand-off)\n        \u2192 pass run artifacts via the job template's artifacts:/needs:, not the binary\n"},
		{"tag-deletion", provider.Capabilities{ContainerTagDeletion: true},
			"  [yes] container tag deletion\n",
			"  [ no] container tag deletion\n        \u2192 container cleanup and rollback cannot delete forge-managed tags\n"},
		{"package-listing", provider.Capabilities{ContainerPackageListing: true},
			"  [yes] container package listing\n",
			"  [ no] container package listing\n        \u2192 container cleanup cannot enumerate stale package versions\n"},
	}
	invert := func(c provider.Capabilities) provider.Capabilities {
		return provider.Capabilities{
			SARIFUpload: !c.SARIFUpload, Attestation: !c.Attestation,
			PublicFulcioTrusted: !c.PublicFulcioTrusted, MintsOIDCToken: !c.MintsOIDCToken,
			ReleaseAssets: !c.ReleaseAssets, RunArtifacts: !c.RunArtifacts,
			ContainerTagDeletion: !c.ContainerTagDeletion, ContainerPackageListing: !c.ContainerPackageListing,
		}
	}

	var enabled, disabled strings.Builder
	enabled.WriteString(header)
	disabled.WriteString(header)

	for _, row := range rows {
		enabled.WriteString(row.on)
		disabled.WriteString(row.off)
	}

	allOn, allOff := enabled.String(), disabled.String()

	type fixture struct {
		name string
		caps provider.Capabilities
		want string
	}

	cases := make([]fixture, 0, 2+2*len(rows))

	cases = append(cases,
		fixture{"all-enabled", invert(provider.Capabilities{}), allOn},
		fixture{"all-disabled", provider.Capabilities{}, allOff},
	)
	for _, row := range rows {
		cases = append(cases,
			fixture{"only-" + row.name, row.only, strings.Replace(allOff, row.off, row.on, 1)},
			fixture{"except-" + row.name, invert(row.only), strings.Replace(allOn, row.on, row.off, 1)},
		)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			appdoctor.FormatEnvironment(&out, appdoctor.Environment{
				Provider: "Fixture Forge", ForgeAPI: "fixture-api", Runner: "fixture-runner", Capabilities: tc.caps,
			})

			if got := out.String(); got != tc.want {
				t.Errorf("capability report:\n%s\nwant exact ordered report:\n%s", got, tc.want)
			}
		})
	}
}
