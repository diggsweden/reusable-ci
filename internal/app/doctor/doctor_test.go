// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/app/doctor"
)

// writeRepo lays out a minimal repo under root with the supplied
// artifacts.yml. Optional extra files via files map (relative path → body).
func writeRepo(t *testing.T, artifactsYAML string, files map[string]string) string {
	t.Helper()

	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".reusable-ci"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, ".reusable-ci", "artifacts.yml"), []byte(artifactsYAML), 0o644); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(body), 0o644); err != nil { //nolint:gosec // test fixture.
			t.Fatal(err)
		}
	}

	return root
}

func findCheck(checks []doctor.Check, name string) *doctor.Check {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}

	return nil
}

func TestRun_HappyPath_GPGDefault(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
`, nil)

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	if exit := doctor.ExitCode(checks); exit != 0 {
		var buf bytes.Buffer
		doctor.FormatText(&buf, checks)
		t.Errorf("expected exit 0, got %d\n%s", exit, buf.String())
	}
}

func TestRun_MissingArtifactsYML_Fails(t *testing.T) {
	root := t.TempDir() // no artifacts.yml

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "artifacts.yml present")
	if c == nil || c.Severity != doctor.SeverityFail {
		t.Errorf("missing artifacts.yml must produce a FAIL check; got %+v", c)
	}

	if !strings.Contains(c.Remediation, "examples/") {
		t.Errorf("remediation must reference examples/; got %q", c.Remediation)
	}
}

func TestRun_RequireAuthorizationWithoutAllowlist_Fails(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
    require-authorization: true
`, nil)

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "release-authorization allowlist present")
	if c == nil || c.Severity != doctor.SeverityFail {
		t.Errorf("require-authorization without allowlist must fail; got %+v", c)
	}
}

func TestRun_RequireAuthorizationWithAllowlist_Passes(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
    require-authorization: true
`, map[string]string{
		".reusable-ci/allowed_signers": "user@example.com ssh-ed25519 AAAA...\n",
	})

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "release-authorization allowlist present")
	if c == nil || c.Severity != doctor.SeverityOK {
		t.Errorf("allowlist present must pass; got %+v", c)
	}
}

func TestRun_SigstoreWithoutIDTokenPermission_Fails(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
sign:
  method: sigstore
`, map[string]string{
		".github/workflows/release.yml": "name: release\non: push\npermissions:\n  contents: write\n",
	})

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "workflow id-token permission")
	if c == nil || c.Severity != doctor.SeverityFail {
		t.Errorf("sigstore + no id-token must fail; got %+v", c)
	}

	if !strings.Contains(c.Remediation, "id-token: write") {
		t.Errorf("remediation must mention id-token: write; got %q", c.Remediation)
	}
}

func TestRun_SigstoreWithIDTokenPermission_Passes(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
sign:
  method: sigstore
`, map[string]string{
		".github/workflows/release.yml": "name: release\non: push\npermissions:\n  contents: write\n  id-token: write\n",
	})

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "workflow id-token permission")
	if c == nil || c.Severity != doctor.SeverityOK {
		t.Errorf("sigstore + id-token present must pass; got %+v", c)
	}
}

func TestRun_FloatingReusableCIRef_Warns(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
`, map[string]string{
		".github/workflows/release.yml": "name: release\non: push\njobs:\n  release:\n    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@main\n",
	})

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	c := findCheck(checks, "workflows pin reusable-ci to a tag")
	if c == nil || c.Severity != doctor.SeverityWarn {
		t.Errorf("@main pin must produce a WARN; got %+v", c)
	}

	if exit := doctor.ExitCode(checks); exit != 0 {
		// Warnings must NOT change exit code — pinned design contract.
		t.Errorf("warning-only checks must not change exit code; got exit %d", exit)
	}
}

func TestRun_KMSWithBadKey_FailsAtSignBlock(t *testing.T) {
	root := writeRepo(t, `
artifacts:
  - name: my-app
    project-type: meta
sign:
  method: kms
  key: /etc/secrets/key
`, nil)

	checks, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	// Config.Validate runs the SignConfig.Validate, which rejects
	// /etc/... — so this comes back as the parses+validates check
	// failing, not as a separate "sign block valid" check.
	c := findCheck(checks, "artifacts.yml validates")
	if c == nil || c.Severity != doctor.SeverityFail {
		t.Errorf("bad sign.key URI must fail validation; got %+v", c)
	}
}

func TestFormatText_RendersAllSeverities(t *testing.T) {
	var buf bytes.Buffer

	doctor.FormatText(&buf, []doctor.Check{
		{Name: "a", Severity: doctor.SeverityOK, Message: "fine"},
		{Name: "b", Severity: doctor.SeverityWarn, Message: "weird", Remediation: "rethink"},
		{Name: "c", Severity: doctor.SeverityFail, Message: "broken", Remediation: "patch"},
	})

	out := buf.String()
	for _, want := range []string{"[OK] a:", "[WARN] b:", "[FAIL] c:", "fix: rethink", "fix: patch"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}
