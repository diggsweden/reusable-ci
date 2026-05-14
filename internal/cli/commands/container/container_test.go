// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"context"
	"strings"
	"testing"

	containercmd "github.com/diggsweden/reusable-ci/internal/cli/commands/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestResolveNameCmd_WritesOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("CONTAINER_REGISTRY", "ghcr.io")
	env.Setenv("REPOSITORY", "owner/repo")
	env.Setenv("REPOSITORY_OWNER", "owner")

	cmd := containercmd.New()
	if err := cmd.Run(context.Background(), []string{"container", "resolve-name"}); err != nil {
		t.Fatal(err)
	}
	if got := env.Output("name"); got != "ghcr.io/owner/repo" {
		t.Errorf("name = %q", got)
	}
}

func TestValidateContainerfileCmd_WritesOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("Containerfile", []byte("FROM alpine\n"))
	cmd := containercmd.New()
	if err := cmd.Run(context.Background(), []string{"container", "validate-containerfile", path}); err != nil {
		t.Fatal(err)
	}
	if got := env.Output("containerfile"); got != path {
		t.Errorf("containerfile = %q", got)
	}
}

func TestCommands_UsageWhenArgsMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	tests := []struct {
		name string
		args []string
	}{
		{name: "validate_containerfile", args: []string{"container", "validate-containerfile"}},
		{name: "validate_artifacts", args: []string{"container", "validate-artifacts", "maven"}},
		{name: "suffix_extracted_binaries", args: []string{"container", "suffix-extracted-binaries", fsys.Root}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := containercmd.New()
			err := cmd.Run(context.Background(), testCase.args)
			if err == nil || !strings.Contains(err.Error(), "Usage") {
				t.Errorf("err = %v", err)
			}
		})
	}
}
