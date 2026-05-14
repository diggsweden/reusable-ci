// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish_test

import (
	"context"
	"strings"
	"testing"

	publishcmd "github.com/diggsweden/reusable-ci/internal/cli/commands/publish"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

func TestValidateAuthCmd_EnvModeAcceptsPassword(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("TARGET_REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "secret")

	cmd := publishcmd.New()
	if err := cmd.Run(context.Background(), []string{"publish", "validate-auth"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAuthCmd_EnvModeFailsWithoutPassword(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("USE_CI_TOKEN", "false")
	env.Setenv("TARGET_REGISTRY", "registry.example.com")
	env.Setenv("CI_REGISTRY", "ghcr.io")
	env.Setenv("REGISTRY_PASSWORD", "")

	cmd := publishcmd.New()
	err := cmd.Run(context.Background(), []string{"publish", "validate-auth"})
	if err == nil || !strings.Contains(err.Error(), "registry-password secret is required") {
		t.Errorf("err = %v", err)
	}
}
