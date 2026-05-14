// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"context"
	"strings"
	"testing"

	validatecmd "github.com/diggsweden/reusable-ci/internal/cli/commands/validate"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestValidateCommands_UsageErrorsBeforeDeps(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "ref-type no args", argv: []string{"validate", "ref-type"}, want: "Usage: ref-type"},
		{name: "tag-format no args", argv: []string{"validate", "tag-format"}, want: "Usage: tag-format"},
		{name: "tag-uniqueness no args", argv: []string{"validate", "tag-uniqueness"}, want: "Usage: tag-uniqueness"},
		{name: "tag-commit no args", argv: []string{"validate", "tag-commit"}, want: "Usage: tag-commit"},
		{name: "tag-signature no args", argv: []string{"validate", "tag-signature"}, want: "Usage: tag-signature"},
		{name: "bot-permissions no args", argv: []string{"validate", "bot-permissions"}, want: "Usage: bot-permissions"},
		{name: "authorization no args", argv: []string{"validate", "authorization"}, want: "Usage: authorization"},
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
	err := cmd.Run(context.Background(), []string{"validate", "workflow-input-defaults", "--root", fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("err = %v", err)
	}
}

func TestRefTypeCmd_SucceedsOnTagRef(t *testing.T) {
	cmd := validatecmd.New()
	if err := cmd.Run(context.Background(), []string{"validate", "ref-type", "tag", "v1.0.0", "refs/tags/v1.0.0"}); err != nil {
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
	if err == nil || !strings.Contains(err.Error(), "Full changelog") {
		t.Errorf("err = %v", err)
	}
}
