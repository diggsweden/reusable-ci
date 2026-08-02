// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"bytes"
	"encoding/json"
	"testing"

	appdoctor "github.com/diggsweden/reusable-ci/v3/internal/app/doctor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestFormatJSON_RoundTripsReport(t *testing.T) {
	t.Parallel()

	report := appdoctor.Report{
		Environment: appdoctor.Environment{
			Provider: "GitHub",
			ForgeAPI: "github",
			Runner:   "github",
			Capabilities: provider.Capabilities{
				SARIFUpload: true, ReleaseAssets: true,
			},
		},
		Checks: []appdoctor.Check{
			{Name: "artifacts.yml present", Severity: appdoctor.SeverityOK, Message: "found"},
			{Name: "sign block", Severity: appdoctor.SeverityFail, Message: "missing", Remediation: "add sign:"},
		},
		Failures: 1,
	}

	var buf bytes.Buffer
	if err := appdoctor.FormatJSON(&buf, report); err != nil {
		t.Fatal(err)
	}

	// Valid JSON, snake_case keys, trailing newline.
	if got := buf.Bytes()[buf.Len()-1]; got != '\n' {
		t.Errorf("want trailing newline, got %q", got)
	}

	var got appdoctor.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got.Failures != 1 || len(got.Checks) != 2 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// Verify the wire keys are the stable snake_case contract a CI gate
	// would parse — not Go field names.
	for _, key := range []string{`"forge_api"`, `"sarif_upload"`, `"run_artifacts"`, `"severity"`, `"failures"`, `"remediation"`} {
		if !bytes.Contains(buf.Bytes(), []byte(key)) {
			t.Errorf("expected JSON key %s in output:\n%s", key, buf.String())
		}
	}

	// An OK check has no remediation, so omitempty must drop it.
	if bytes.Count(buf.Bytes(), []byte(`"remediation"`)) != 1 {
		t.Errorf("remediation should appear once (omitempty on the OK check):\n%s", buf.String())
	}
}

func TestCountFailures(t *testing.T) {
	t.Parallel()

	checks := []appdoctor.Check{
		{Severity: appdoctor.SeverityOK},
		{Severity: appdoctor.SeverityWarn},
		{Severity: appdoctor.SeverityFail},
		{Severity: appdoctor.SeverityFail},
	}
	if got := appdoctor.CountFailures(checks); got != 2 {
		t.Errorf("CountFailures = %d, want 2", got)
	}
}
