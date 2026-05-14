// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sbomcmd "github.com/diggsweden/reusable-ci/internal/cli/commands/sbom"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestGenerateCmd_InvalidProjectTypeFailsFast(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	cmd := sbomcmd.New()
	err := cmd.Run(context.Background(), []string{"sbom", "generate", "--project-type", "unknown"})
	if err == nil || !strings.Contains(err.Error(), `invalid --project-type "unknown"`) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("expected valid-type list, got: %v", err)
	}
}

func TestGenerateContainerCmd_RequiresRefNameFromEnv(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("CI_REF_NAME", "")
	env.Setenv("GITHUB_REF_NAME", "")
	cmd := sbomcmd.New()
	err := cmd.Run(context.Background(), []string{"sbom", "generate-container"})
	if err == nil || !strings.Contains(err.Error(), "CI_REF_NAME is required") {
		t.Errorf("err = %v", err)
	}
}

func TestGenerateContainerCmd_UsesGitHubEnvInputs(t *testing.T) {
	mock := mockbinary.New(t)
	mock.Add("syft", `
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-o" ]]; then
    next=$((i+1))
    output="${!next}"
    printf '{}' > "${output#*=}"
  fi
done
`)

	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.Chdir()
	env := testenv.New(t)
	env.Setenv("CI_REF_NAME", "")
	env.Setenv("CI_REPO", "")
	env.Setenv("GITHUB_REF_NAME", "v2.5.3")
	env.Setenv("GITHUB_REPOSITORY", "my-org/my-service")
	env.Setenv("ARTIFACT_TYPES", "maven")
	env.Setenv("IMAGE_NAME", "registry.example.com/team/app")
	env.Setenv("IMAGE_DIGEST", "sha256:deadbeef0123")

	cmd := sbomcmd.New()
	if err := cmd.Run(context.Background(), []string{"sbom", "generate-container"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"my-service-2.5.3-analyzed-container-sbom.spdx.json",
		"my-service-2.5.3-analyzed-container-sbom.cyclonedx.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	calls := mock.Invocations("syft")
	if len(calls) != 1 {
		t.Fatalf("expected 1 syft call, got %d", len(calls))
	}
	if got := calls[0].Args[0]; got != "registry.example.com/team/app@sha256:deadbeef0123" {
		t.Errorf("syft target = %q", got)
	}
}

func TestFindContainerSBOMCmd_WritesGitHubOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	const target = "demo-analyzed-container-sbom.spdx.json"
	fsys.WriteFile(target, []byte("{}"))

	cmd := sbomcmd.New()
	if err := cmd.Run(context.Background(), []string{"sbom", "find-container-sbom"}); err != nil {
		t.Fatal(err)
	}
	if got := env.Output("sbom-file"); got != target {
		t.Errorf("sbom-file = %q, want %q", got, target)
	}
}
