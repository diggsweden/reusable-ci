// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	validatecmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestValidateCommands_UsageErrorsBeforeDeps(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "tag-format no flags", argv: []string{"validate", "tag", "format"}, want: `Required flag "tag" not set`}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "tag-uniqueness no flags", argv: []string{"validate", "tag", "uniqueness"}, want: `Required flag "tag" not set`},
		{name: "tag-commit no flags", argv: []string{"validate", "tag", "commit"}, want: `Required flag "tag" not set`},
		{name: "tag-signature no flags", argv: []string{"validate", "tag", "signature"}, want: `Required flag "tag" not set`},
		{name: "auth bot-permissions no flags", argv: []string{"validate", "auth", "bot-permissions"}, want: `Required flag "repository" not set`}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := validatecmd.New()

			err := cmd.Run(context.Background(), testCase.argv)
			if err == nil {
				t.Fatal("expected a refusal")
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want substring %q", err, testCase.want)
			}

			// urfave's own required-flag errors carry no sentinel until
			// main.go classifies them, so the exit code -- the thing the
			// operator's shell sees -- is pinned through that same function.
			if got := errs.ExitCodeFromError(cli.ClassifyError(err)); got != errs.ExitCodeUsage {
				t.Errorf("exit code = %d, want usage (%d)", got, errs.ExitCodeUsage)
			}
		})
	}
}

func TestWorkflowInputDefaultsCmd_ReportsValidationFailure(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(".github/workflows/bad.yml", []byte("on:\n  workflow_call:\n    inputs:\n      ref:\n        default: ${{ github.ref_name }}\n"))

	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "workflow", "input-defaults", "--root", fsys.Root})
	// A workflow that breaks the rule is a domain-rule failure (exit 1).
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("err = %v, want it to say what failed", err)
	}
}

func TestChangelogCmd_MinimalModeWritesFallbackContent(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	missing := fsys.Path("missing.md")

	cmd := validatecmd.New()
	if err := cmd.Run(context.Background(), []string{"validate", "changelog", "--path", missing}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("content"); got != "No changes for this release" {
		t.Errorf("content = %q", got)
	}
}

func TestChangelogCmd_RequiredModeFailsWhenMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	missing := fsys.Path("missing.md")
	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "changelog", "--path", missing, "--required"})
	// A required file that is absent is missing input (66): add the file.
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}

	if !strings.Contains(err.Error(), "full changelog") {
		t.Errorf("err = %v, want it to name the missing document", err)
	}
}

func TestAuthRegistryCmd_EnvModeAcceptsPassword(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "secret")

	cmd := validatecmd.New()
	if err := cmd.Run(context.Background(), []string{"validate", "auth", "registry"}); err != nil {
		t.Fatal(err)
	}
}

// TestAuthRegistryCmd_EnvModeAcceptsRegistryToken pins that the presence
// check honours $REGISTRY_TOKEN, the name `container login` resolves first,
// so a pipeline that logs in with it is not refused by the pre-flight.
func TestAuthRegistryCmd_EnvModeAcceptsRegistryToken(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")
	env.Setenv("REGISTRY_TOKEN", "secret")

	cmd := validatecmd.New()
	if err := cmd.Run(context.Background(), []string{"validate", "auth", "registry"}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthRegistryCmd_EnvModeFailsWithoutPassword(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")

	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "auth", "registry"})
	// A missing secret is a credential problem (77), not a bad flag.
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if !strings.Contains(err.Error(), "registry-password secret is required") {
		t.Errorf("err = %v, want it to name the missing secret", err)
	}
}
