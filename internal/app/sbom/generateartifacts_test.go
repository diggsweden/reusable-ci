// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestGenerateArtifacts_ConfigPlanUsesArtifactWorkingDirectories(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("packages", "app", "bom.json"), []byte(`{"bomFormat":"CycloneDX","name":"app"}`))
	fsys.WriteFile("bom.json", []byte(`{"bomFormat":"CycloneDX"}`))
	fsys.Chdir()

	configPlanJSON := sbomPlanJSON(t, sbomConfigPlan(t,
		config.Artifact{Name: "app", ProjectType: projecttype.NPM, WorkingDirectory: "packages/app", SBOMs: "build"},
		config.Artifact{Name: "docs", ProjectType: projecttype.Meta, WorkingDirectory: "docs", SBOMs: "none"}))
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
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "config-plan-json is required") {
		t.Fatalf("err = %v, want ErrUsage naming the missing plan", err)
	}
}

func TestGenerateArtifacts_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	err := appsbom.GenerateArtifacts(context.Background(), &fakeSyft{}, nil, &fakeGit{}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{
		ConfigPlanJSON: `{"version":2,"artifacts":{"all":[]},"containers":{"all":[],"has_containers":false}}`,
		SBOMs:          "build",
	})
	// ErrInvalidConfig: the plan comes from an earlier step, so a version
	// this binary cannot read is skew between steps, not a bad flag.
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "unsupported version 2") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming the version", err)
	}
}

// TestGenerateArtifacts_RefusesUnusableConfigPlansWithoutSideEffects covers
// the plans between "absent" and "a supported plan with the wrong version",
// which were the only two refusals under test.
//
// The plan is produced by an earlier workflow step and handed over as one
// string, so every way that string can be wrong arrives here rather than at a
// flag parser: truncated by a shell, JSON of the wrong shape, or a plan whose
// contents do not hold together. All of them have to fail the same way, and
// two consequences matter beyond the error value. Syft must not be invoked —
// it is an external scan over a source tree, started before anything has
// established what to scan — and nothing may be written, because a partial
// SBOM left in the working tree is collected by a later publish step as if it
// were a complete one.
func TestGenerateArtifacts_RefusesUnusableConfigPlansWithoutSideEffects(t *testing.T) {
	valid := sbomPlanJSON(t, sbomConfigPlan(t,
		config.Artifact{Name: "app", ProjectType: projecttype.NPM, WorkingDirectory: ".", SBOMs: "build"}))

	for name, plan := range map[string]string{
		"a truncated object":       valid[:len(valid)/2],
		"an array":                 `[{"version":1}]`,
		"a bare string":            `"config-plan"`,
		"a number":                 `7`,
		"an unknown member":        `{"version":1,"artifacts":{"all":[]},"containers":{"all":[],"has_containers":false},"surprise":true}`,
		"artifacts of wrong type":  `{"version":1,"artifacts":"all","containers":{"all":[],"has_containers":false}}`,
		"an artifact with no name": `{"version":1,"artifacts":{"all":[{"name":"","project_type":"npm"}]},"containers":{"all":[],"has_containers":false}}`,
		"an unsupported type":      `{"version":1,"artifacts":{"all":[{"name":"app","project_type":"cobol"}]},"containers":{"all":[],"has_containers":false}}`,
	} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			syft := &fakeSyft{}

			err := appsbom.GenerateArtifacts(context.Background(), syft, nil, &fakeGit{sha: "abc1234"}, io.Discard, io.Discard, appsbom.GenerateArtifactsInput{
				ConfigPlanJSON: plan,
				SBOMs:          "build",
				Version:        "1.2.3",
			})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			// Every refusal names the input the operator has to correct. The
			// plan is one of several JSON strings a step is handed, so an
			// error that only says "invalid JSON" does not identify which.
			if !strings.Contains(err.Error(), "config-plan") {
				t.Errorf("the refusal does not name config-plan:\n%v", err)
			}

			if len(syft.calls) != 0 {
				t.Errorf("syft ran %d time(s) for a plan that was refused: %+v", len(syft.calls), syft.calls)
			}

			left, readErr := os.ReadDir(fsys.Root)
			if readErr != nil {
				t.Fatalf("read working tree: %v", readErr)
			}

			if len(left) != 0 {
				names := make([]string, 0, len(left))
				for _, e := range left {
					names = append(names, e.Name())
				}

				t.Errorf("a refused plan left %v in the working tree", names)
			}
		})
	}
}

func TestGenerateArtifacts_ConfigPlanIntersectsReleaseAndArtifactSBOMLayers(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("packages", "build-only", "bom.json"), []byte(`{"bomFormat":"CycloneDX","name":"build-only"}`))
	fsys.WriteFile(filepath.Join("packages", "artifact-only", "package.json"), []byte(`{"name":"artifact-only","version":"1.2.3"}`))
	fsys.WriteFile(filepath.Join("packages", "none", "bom.json"), []byte(`{"bomFormat":"CycloneDX","name":"none"}`))
	fsys.WriteFile(filepath.Join("packages", "artifact-only", "artifact-only-1.2.3.tgz"), []byte("tarball"))
	fsys.Chdir()

	configPlanJSON := sbomPlanJSON(t, sbomConfigPlan(t,
		config.Artifact{Name: "build-only", ProjectType: projecttype.NPM, WorkingDirectory: "packages/build-only", SBOMs: "build"},
		config.Artifact{Name: "artifact-only", ProjectType: projecttype.NPM, WorkingDirectory: "packages/artifact-only", SBOMs: "analyzed-artifact"},
		config.Artifact{Name: "none", ProjectType: projecttype.NPM, WorkingDirectory: "packages/none", SBOMs: "none"}))
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
