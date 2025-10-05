// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	validatecmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestValidateCommands_EachRefusalHasItsExitClassAndNoEffects runs validators
// end to end through the command tree, each against an owned tree that fails
// for one reason, and pins the exit code the shell sees (through the same
// classification main uses), the cause named in the error, and that nothing
// was written to the step outputs or summary. A rule violation exits 1, a
// malformed workflow 65, a missing input 66, a malformed plan 78 and a missing
// secret 77, so an operator can tell which kind of fix a failure needs.
//
// Not parallel: ghaenv and the registry case set process environment.
func TestValidateCommands_EachRefusalHasItsExitClassAndNoEffects(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("malformed/.github/workflows/broken.yml", []byte("jobs: [\n"))
	fsys.WriteFile("expression/.github/workflows/a.yml", []byte("on:\n  workflow_call:\n    inputs:\n      ref:\n        default: ${{ github.ref_name }}\n"))

	tests := []struct {
		name  string
		argv  []string
		env   map[string]string
		exit  errs.ExitCodeType
		cause string
	}{
		{name: "tag format", argv: []string{"validate", "tag", "format", "--tag", "not-a-tag"}, exit: errs.ExitCodeValidation, cause: `invalid tag format: "not-a-tag"`},
		{name: "expression default", argv: []string{"validate", "workflow", "input-defaults", "--root", fsys.Path("expression")}, exit: errs.ExitCodeValidation, cause: "workflow input defaults validation failed"},
		{name: "malformed workflow", argv: []string{"validate", "job-graph", "--root", fsys.Path("malformed")}, exit: errs.ExitCodeDataErr, cause: "broken.yml: parse workflow yaml"},
		{name: "missing workflow file", argv: []string{"validate", "job-graph", "--root", fsys.Root, "--workflow", fsys.Path("missing.yml")}, exit: errs.ExitCodeNoInput, cause: "missing.yml"},
		{name: "missing workflow directory", argv: []string{"validate", "workflow", "input-defaults", "--root", fsys.Path("absent")}, exit: errs.ExitCodeNoInput, cause: ".github/workflows"},
		{name: "missing required changelog", argv: []string{"validate", "changelog", "--path", fsys.Path("missing.md"), "--required"}, exit: errs.ExitCodeNoInput, cause: "full changelog"},
		{name: "malformed config plan", argv: []string{"validate", "cargo", "--config-plan-json", "{"}, exit: errs.ExitCodeConfiguration, cause: "parse config-plan-json"},
		{
			name: "missing registry secret", argv: []string{"validate", "auth", "registry"}, exit: errs.ExitCodeNoPerm, cause: "registry-password secret is required",
			env: map[string]string{"USE_CI_TOKEN": "false", "REGISTRY": "registry.example.com", "CI_REGISTRY": "ghcr.io", "REGISTRY_PASSWORD": ""},
		},
		{name: "missing required flag", argv: []string{"validate", "tag", "commit"}, exit: errs.ExitCodeUsage, cause: `Required flag "tag" not set`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := ghaenv.Setup(t)
			for key, value := range tc.env {
				env.Setenv(key, value)
			}

			err := validatecmd.New().Run(context.Background(), tc.argv)
			if got := errs.ExitCodeFromError(cli.ClassifyError(err)); got != tc.exit {
				t.Errorf("exit = %d (%v), want %d", got, err, tc.exit)
			}

			if err == nil || !strings.Contains(err.Error(), tc.cause) {
				t.Errorf("err = %v, want it to name %q", err, tc.cause)
			}

			for _, path := range []string{env.OutputPath, env.SummaryPath} {
				if data, readErr := os.ReadFile(path); readErr != nil || len(data) != 0 {
					t.Errorf("refusal wrote %q to %s (err %v)", data, path, readErr)
				}
			}
		})
	}
}
