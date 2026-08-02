// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"strings"
	"testing"

	containercmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestRefResolveCmd_WritesOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("CONTAINER_REGISTRY", "ghcr.io")
	env.Setenv("REPOSITORY", "owner/repo")
	env.Setenv("REPOSITORY_OWNER", "owner")

	cmd := containercmd.New()
	if err := cmd.Run(context.Background(), []string{"container", "ref", "resolve"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
	if err := cmd.Run(context.Background(), []string{"container", "validate", "containerfile", "--path", path}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("containerfile"); got != path {
		t.Errorf("containerfile = %q", got)
	}
}

func TestCommands_RequireFlagsWhenMissing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "validate_containerfile", args: []string{"container", "validate", "containerfile"}, want: `Required flag "path" not set`},
		{name: "validate_artifacts_missing_artifact_dir", args: []string{"container", "validate", "artifacts", "--project-type", "maven"}, want: `Required flag "artifact-dir" not set`},
		{name: "suffix_extracted_binaries_missing_arch", args: []string{"container", "suffix-extracted-binaries", "--binaries-dir", "./extracted"}, want: `Required flag "arch" not set`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := containercmd.New()

			err := cmd.Run(context.Background(), testCase.args)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want substring %q", err, testCase.want)
			}
		})
	}
}
