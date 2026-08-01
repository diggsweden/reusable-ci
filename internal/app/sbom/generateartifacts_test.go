// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestGenerateArtifacts_ConfigPlanUsesArtifactWorkingDirectories(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("packages", "app", "bom.json"), []byte(`{"name":"app"}`))
	fsys.WriteFile("bom.json", []byte(`{"name":"root"}`))
	fsys.Chdir()

	configPlanJSON := `{"version":1,"artifacts":{"all":[{"name":"app","project_type":"npm","working_directory":"packages/app","effective_sboms":["build"]},{"name":"docs","project_type":"meta","working_directory":"docs"}]},"containers":{"all":[],"has_containers":false},"pipeline_sboms":"all"}`
	if err := appsbom.GenerateArtifacts(context.Background(), &fakeSyft{}, nil, &fakeGit{sha: "abc1234"}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ConfigPlanJSON: configPlanJSON,
		SBOMs:          "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Version:        "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join("packages", "app", "app-1.2.3-abc1234-build-sbom.cyclonedx.json")
	if _, err := os.Stat(fsys.Path(want)); err != nil {
		t.Fatalf("expected %s: %v", want, err)
	}

	if _, err := os.Stat(fsys.Path("app-1.2.3-abc1234-build-sbom.cyclonedx.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected root-level SBOM, err=%v", err)
	}
}

func TestGenerateArtifacts_RequiresConfigPlan(t *testing.T) {
	t.Parallel()

	err := appsbom.GenerateArtifacts(context.Background(), &fakeSyft{}, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{SBOMs: "build"})
	if err == nil || !strings.Contains(err.Error(), "config-plan-json is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateArtifacts_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	err := appsbom.GenerateArtifacts(context.Background(), &fakeSyft{}, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{
		ConfigPlanJSON: `{"version":2,"artifacts":{"all":[]},"containers":{"all":[],"has_containers":false}}`,
		SBOMs:          "build",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported version 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateArtifacts_ConfigPlanIntersectsReleaseAndArtifactSBOMLayers(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("packages", "build-only", "bom.json"), []byte(`{"name":"build-only"}`))
	fsys.WriteFile(filepath.Join("packages", "artifact-only", "package.json"), []byte(`{"name":"artifact-only","version":"1.2.3"}`))
	fsys.WriteFile(filepath.Join("packages", "none", "bom.json"), []byte(`{"name":"none"}`))
	fsys.WriteFile(filepath.Join("packages", "artifact-only", "artifact-only-1.2.3.tgz"), []byte("tarball"))
	fsys.Chdir()

	configPlanJSON := `{"version":1,"artifacts":{"all":[{"name":"build-only","project_type":"npm","working_directory":"packages/build-only","effective_sboms":["build"]},{"name":"artifact-only","project_type":"npm","working_directory":"packages/artifact-only","effective_sboms":["analyzed-artifact"]},{"name":"none","project_type":"npm","working_directory":"packages/none"}]},"containers":{"all":[],"has_containers":false},"pipeline_sboms":"all"}`
	if err := appsbom.GenerateArtifacts(context.Background(), &fakeSyft{}, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{
		ConfigPlanJSON: configPlanJSON,
		SBOMs:          "build,analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Version:        "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		filepath.Join("packages", "build-only", "build-only-1.2.3-build-sbom.cyclonedx.json"),
		filepath.Join("packages", "artifact-only", "artifact-only-1.2.3-analyzed-tararchive-sbom.cyclonedx.json"),
	} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("expected %s: %v", want, err)
		}
	}

	if _, err := os.Stat(fsys.Path("packages", "none", "none-1.2.3-build-sbom.cyclonedx.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected SBOM for none artifact, err=%v", err)
	}
}
