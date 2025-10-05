// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/stretchr/testify/require"
)

func TestParseTrivyReport_RejectsObjectsWithoutReportEvidence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"empty-object", `{}`},
		{"unknown-only", `{"unexpected":true}`},
		{"metadata-only", `{"Metadata":{"OS":{"Family":"alpine"}}}`},
		{"unnamed-null-results", `{"Results":null}`},
		{"empty-name", `{"ArtifactName":""}`},
		{"blank-name", `{"ArtifactName":" \t\r\n"}`},
		{"blank-name-null-results", `{"ArtifactName":" \t","Results":null}`},
		{"null-name", `{"ArtifactName":null}`},
		{"null-report", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := security.ParseTrivyReport([]byte(tc.body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, report)
		})
	}
}

func TestParseTrivyReport_AcceptsSupportedCleanSubsets(t *testing.T) {
	t.Parallel()
	// Synthetic consumed-field subsets, not captured native-version fixtures.
	for _, tc := range []struct {
		name, body string
		want       security.TrivyReport
	}{
		{"empty-array", `{"Results":[]}`, security.TrivyReport{Results: []security.TrivyResult{}}},
		{"blank-name-empty-array", `{"ArtifactName":" \t","Results":[]}`, security.TrivyReport{ArtifactName: " \t", Results: []security.TrivyResult{}}},
		{"unknown-fields", `{"Results":[],"Future":{"nested":[true,1]}}`, security.TrivyReport{Results: []security.TrivyResult{}}},
		{"named-omitted", `{"ArtifactName":"image"}`, security.TrivyReport{ArtifactName: "image"}},
		{"named-null", `{"ArtifactName":"image","Results":null}`, security.TrivyReport{ArtifactName: "image"}},
		{"name-preserved", `{"ArtifactName":" image \t","Future":true}`, security.TrivyReport{ArtifactName: " image \t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := security.ParseTrivyReport([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, &tc.want, report)
		})
	}
}

func TestParseTrivyReport_RetainsJSONDecodeCauses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		syntax     bool
	}{
		{"empty", ``, true},
		{"truncated", `{"Results":[`, true},
		{"trailing-document", `{"Results":[]} {}`, true},
		{"array", `[]`, false},
		{"string", `"report"`, false},
		{"number", `42`, false},
		{"boolean", `true`, false},
		{"artifact-name", `{"ArtifactName":42,"Results":[]}`, false},
		{"results-object-with-name", `{"ArtifactName":"image","Results":{}}`, false},
		{"result-element", `{"Results":[42]}`, false},
		{"target", `{"Results":[{"Target":42}]}`, false},
		{"metadata", `{"ArtifactName":"image","Metadata":[]}`, false},
		{"os-name", `{"Results":[],"Metadata":{"OS":{"Name":42}}}`, false},
		{"vulnerabilities", `{"Results":[{"Vulnerabilities":{}}]}`, false},
		{"severity", `{"Results":[{"Vulnerabilities":[{"Severity":42}]}]}`, false},
		{"references", `{"Results":[{"Vulnerabilities":[{"References":[42]}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := security.ParseTrivyReport([]byte(tc.body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, report)

			if tc.syntax {
				var cause *json.SyntaxError
				require.ErrorAs(t, err, &cause)
			} else {
				var cause *json.UnmarshalTypeError
				require.ErrorAs(t, err, &cause)
			}
		})
	}
}

func TestParseTrivyReport_PreservesConsumedFieldsWithUnknownFields(t *testing.T) {
	t.Parallel()

	report, err := security.ParseTrivyReport([]byte(`{
		"ArtifactName":"image", "SchemaVersion":2, "ArtifactType":"container_image",
		"Metadata":{"OS":{"Family":"alpine","Name":"3.21","Future":true},"Future":{}},
		"Results":[{"Target":"target","Class":"os-pkgs","Future":[],"Vulnerabilities":[{
			"VulnerabilityID":"CVE-2026-1234","PkgName":"pkg","InstalledVersion":"1","FixedVersion":"2",
			"Title":"title","Description":"description","Severity":"HIGH",
			"PrimaryURL":"https://example.invalid/advisory","References":["https://example.invalid/ref"],"Future":true
		}]}], "Future":true
	}`))
	require.NoError(t, err)
	require.Equal(t, &security.TrivyReport{
		ArtifactName: "image",
		Metadata:     &security.TrivyMetadata{OS: &security.TrivyOS{Family: "alpine", Name: "3.21"}},
		Results: []security.TrivyResult{{Target: "target", Class: "os-pkgs", Vulnerabilities: []security.TrivyVulnerability{{
			VulnerabilityID: "CVE-2026-1234", PkgName: "pkg", InstalledVersion: "1", FixedVersion: "2",
			Title: "title", Description: "description", Severity: "HIGH",
			PrimaryURL: "https://example.invalid/advisory", References: []string{"https://example.invalid/ref"},
		}}}},
	}, report)
}
