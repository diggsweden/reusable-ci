// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sbomcmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestAssembleCmd_InvalidProjectTypeFailsFast(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	cmd := sbomcmd.New()

	err := cmd.Run(context.Background(), []string{"sbom", "assemble", "--project-type", "unknown"})
	if err == nil || !strings.Contains(err.Error(), `invalid --project-type "unknown"`) {
		t.Fatalf("err = %v", err)
	}

	if !strings.Contains(err.Error(), "valid:") {
		t.Errorf("expected valid-type list, got: %v", err)
	}
}

func TestBuildCmd_UnsupportedEcoIsUsageError(t *testing.T) {
	cmd := sbomcmd.New()

	// maven's build BOM is a build byproduct, not a standalone generate.
	err := cmd.Run(context.Background(), []string{"sbom", "build", "--project-type", "maven"})
	if err == nil || !strings.Contains(err.Error(), "byproduct of `build maven run`") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildCmd_GoGeneratesBuildBOM(t *testing.T) {
	mock := mockbinary.New(t)
	// cyclonedx-gomod ... -output <dir>/bom.json . — create the output file.
	mock.Add("cyclonedx-gomod", `
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-output" ]]; then
    next=$((i+1)); printf '{}' > "${!next}"
  fi
done
`)

	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.Chdir()

	cmd := sbomcmd.New()
	if err := cmd.Run(context.Background(), []string{"sbom", "build", "--project-type", "go", "--name", "myapp"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".reusable-ci", "go-build-sbom", "myapp", "bom.json")); err != nil {
		t.Errorf("expected generated bom.json: %v", err)
	}
}

func TestAssembleContainerMode_RequiresRefNameFromEnv(t *testing.T) {
	env := testenv.New(t)
	// IMAGE_* selects assemble's container mode; missing ref name then errors.
	env.Setenv("IMAGE_NAME", "registry/app")
	env.Setenv("IMAGE_DIGEST", "sha256:abc")
	env.Setenv("CI_REF_NAME", "")
	env.Setenv("GITHUB_REF_NAME", "")
	env.Setenv("FORGEJO_REF_NAME", "")

	cmd := sbomcmd.New()

	err := cmd.Run(context.Background(), []string{"sbom", "assemble"})
	if err == nil || !strings.Contains(err.Error(), "ref name is required") {
		t.Errorf("err = %v", err)
	}
}

func TestAssembleContainerMode_UsesGitHubEnvInputs(t *testing.T) {
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
	if err := cmd.Run(context.Background(), []string{"sbom", "assemble"}); err != nil {
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

func TestAssembleCmd_InvalidSignMethodIsUsageError(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	cmd := sbomcmd.New()

	err := cmd.Run(context.Background(), []string{"sbom", "assemble", "--sign", "--sign-method", "bogus"})
	if err == nil || !strings.Contains(err.Error(), `--sign-method must be "sigstore" or "kms"`) {
		t.Fatalf("err = %v", err)
	}
}
