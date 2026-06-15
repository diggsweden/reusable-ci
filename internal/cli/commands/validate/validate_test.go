// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	"strings"
	"testing"

	validatecmd "github.com/diggsweden/reusable-ci/internal/cli/commands/validate"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestValidateCommands_UsageErrorsBeforeDeps(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "ref-type no flags", argv: []string{"validate", "ref-type"}, want: `Required flags "ref-type, ref-name" not set`}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "tag-format no flags", argv: []string{"validate", "tag", "format"}, want: `Required flag "tag" not set`},          //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "tag-uniqueness no flags", argv: []string{"validate", "tag", "uniqueness"}, want: `Required flag "tag" not set`},
		{name: "tag-commit no flags", argv: []string{"validate", "tag", "commit"}, want: `Required flag "tag" not set`},
		{name: "tag-signature no flags", argv: []string{"validate", "tag", "signature"}, want: `Required flag "tag" not set`},
		{name: "auth bot-permissions no flags", argv: []string{"validate", "auth", "bot-permissions"}, want: `Required flag "repository" not set`}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := validatecmd.New()

			err := cmd.Run(context.Background(), testCase.argv)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want substring %q", err, testCase.want)
			}
		})
	}
}

func TestWorkflowInputDefaultsCmd_ReportsValidationFailure(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(".github/workflows/bad.yml", []byte("      default: ${{ github.ref_name }}\n"))

	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "workflow", "input-defaults", "--root", fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("err = %v", err)
	}
}

func TestContractResidueCmd_ReportsValidationFailure(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(".github/workflows/bad.yml", []byte("value: ${{ steps.meta.outputs."+"VERSION }}\n"))

	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "workflow", "contract-residue", "--root", fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "v3 contract validation failed") {
		t.Errorf("err = %v", err)
	}
}

func TestRefTypeCmd_SucceedsOnTagRef(t *testing.T) {
	cmd := validatecmd.New()
	if err := cmd.Run(context.Background(), []string{
		"validate", "ref-type",
		"--ref-type", "tag",
		"--ref-name", "v1.0.0",
		"--ref", "refs/tags/v1.0.0",
	}); err != nil {
		t.Fatal(err)
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
	if err == nil || !strings.Contains(err.Error(), "full changelog") {
		t.Errorf("err = %v", err)
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

func TestAuthRegistryCmd_EnvModeFailsWithoutPassword(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")

	cmd := validatecmd.New()

	err := cmd.Run(context.Background(), []string{"validate", "auth", "registry"})
	if err == nil || !strings.Contains(err.Error(), "registry-password secret is required") {
		t.Errorf("err = %v", err)
	}
}
