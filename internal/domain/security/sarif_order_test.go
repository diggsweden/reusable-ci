// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTrivySARIF_EquivalentOrderHasIdenticalBytes(t *testing.T) {
	t.Parallel()

	a := security.TrivyVulnerability{VulnerabilityID: "CVE-fixture", Title: "same", Description: "alpha", Severity: "HIGH", PkgName: "pkg", InstalledVersion: "1", PrimaryURL: "https://a.invalid"}
	b := a
	b.Description = "beta"
	b.Severity = "LOW"
	b.PrimaryURL = "https://b.invalid"
	first := &security.TrivyReport{Results: []security.TrivyResult{{Vulnerabilities: []security.TrivyVulnerability{a, b}}}}
	second := &security.TrivyReport{Results: []security.TrivyResult{{Vulnerabilities: []security.TrivyVulnerability{b, a}}}}
	left, err := json.Marshal(security.TrivyToSARIF(first, security.Options{}))
	require.NoError(t, err)
	right, err := json.Marshal(security.TrivyToSARIF(second, security.Options{}))
	require.NoError(t, err)
	require.Equal(t, string(left), string(right))
	require.Equal(t, b, second.Results[0].Vulnerabilities[0])
}
