// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package mise_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/mise"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestRun_ReturnsTrimmedOutput(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("mise", `printf 'mise 2024.1.0\n\n'`)

	got, err := mise.New().Run(context.Background(), nil, "--version")
	if err != nil {
		t.Fatal(err)
	}

	// Trailing newlines are stripped so callers can compare a version
	// string without trimming at every call site.
	if got != "mise 2024.1.0" {
		t.Errorf("output = %q, want %q", got, "mise 2024.1.0")
	}
}

// TestRun_EnvReplacesRatherThanExtends pins a contract that differs from
// the gotool adapter next door, where in.Env is appended to the ambient
// environment. Here a non-nil env is the whole environment: passing one
// is how a caller gets a mise run that cannot see the parent's variables.
//
// The stub is invoked by absolute path via SetBin, so the replaced
// environment does not have to carry a PATH for the lookup to work.
func TestRun_EnvReplacesRatherThanExtends(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "mise-stub")

	script := "#!/bin/sh\nprintf 'marker=%s custom=%s\\n' \"${AMBIENT_MARKER:-unset}\" \"${CUSTOM:-unset}\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { //nolint:gosec // test-owned stub must be executable.
		t.Fatal(err)
	}

	t.Setenv("AMBIENT_MARKER", "present")

	runner := mise.New()
	runner.SetBin(stub)

	// nil → inherit the parent environment.
	inherited, err := runner.Run(context.Background(), nil, "x")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(inherited, "marker=present") {
		t.Errorf("nil env should inherit the parent environment: %q", inherited)
	}

	// non-nil → that environment and nothing else.
	replaced, err := runner.Run(context.Background(), []string{"CUSTOM=yes"}, "x")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(replaced, "marker=unset") {
		t.Errorf("non-nil env should replace, not extend: %q", replaced)
	}

	if !strings.Contains(replaced, "custom=yes") {
		t.Errorf("supplied env not applied: %q", replaced)
	}
}

// TestRun_FailureOutputIsRedacted covers the reason the error path goes
// through RedactKeyMaterial: mise resolves tools from registries and its
// failure output can echo a token or a key it was handed. That output is
// folded into the returned error, which lands in the CI log.
func TestRun_FailureOutputIsRedacted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		script     string
		wantHidden string
		wantMarker string
	}{
		{
			name:       "private key in the output",
			script:     `printf -- '-----BEGIN OPENSSH PRIVATE KEY-----\nsecretbody\n'; exit 1`,
			wantHidden: "secretbody",
			wantMarker: "output redacted",
		},
		{
			// A header carrying "typ" clears the pattern's length floor.
			// The bare-{"alg"} variant does not and is propagated
			// unredacted -- covered in internal/safeexec/redact_test.go
			// and recorded in docs/open-questions.md.
			name:       "jwt-shaped token in the output",
			script:     `printf 'auth failed for eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk\n'; exit 1`,
			wantHidden: "eyJzdWIiOiIxMjM0NTY3ODkw",
			wantMarker: "output redacted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t)
			m.Add("mise", tc.script)

			out, err := mise.New().Run(context.Background(), nil, "install")
			if err == nil {
				t.Fatal("expected an error")
			}

			if out != "" {
				t.Errorf("a failed run must return no output, got %q", out)
			}

			if strings.Contains(err.Error(), tc.wantHidden) {
				t.Errorf("secret material reached the error: %v", err)
			}

			if !strings.Contains(err.Error(), tc.wantMarker) {
				t.Errorf("error should say the output was redacted: %v", err)
			}
		})
	}
}

// TestRun_FailureKeepsOrdinaryOutput checks the other side: output with
// nothing sensitive in it is preserved, since that is what makes a failed
// tool install diagnosable.
func TestRun_FailureKeepsOrdinaryOutput(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("mise", `printf 'no such tool: nope@1.2.3\n'; exit 1`)

	if _, err := mise.New().Run(context.Background(), nil, "install", "nope@1.2.3"); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "no such tool: nope@1.2.3") {
		t.Errorf("ordinary output should survive: %v", err)
	}
}
