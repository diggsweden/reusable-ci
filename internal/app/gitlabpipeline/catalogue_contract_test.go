// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"gopkg.in/yaml.v3"
)

func TestGeneratedInputsMatchCatalogueComponentContracts(t *testing.T) {
	t.Parallel()

	includeAAB := false
	enableSigning := true

	build, err := gitlabpipeline.BuildStagePipeline(pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
		Maven:         runningTarget(pipeline.PlannedArtifact{Name: "maven", ProjectType: projecttype.Maven}),
		NPM:           runningTarget(pipeline.PlannedArtifact{Name: "npm", ProjectType: projecttype.NPM}),
		Gradle:        runningTarget(pipeline.PlannedArtifact{Name: "gradle", ProjectType: projecttype.Gradle}),
		GradleAndroid: runningTarget(pipeline.PlannedArtifact{Name: "android", ProjectType: projecttype.GradleAndroid, GradleAndroid: &config.GradleAndroidConfig{IncludeAAB: &includeAAB, EnableAndroidSigning: &enableSigning}}),
		Go:            runningTarget(pipeline.PlannedArtifact{Name: "go", ProjectType: projecttype.Go}),
		Cargo:         runningTarget(pipeline.PlannedArtifact{Name: "cargo", ProjectType: projecttype.Cargo}),
	}}, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "catalog.example/components",
		ComponentRef:  "1.0.0",
		Version:       "1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, include := range build.Include {
		assertCatalogueContract(t, include)
	}
}

func assertCatalogueContract(t *testing.T, include gitlabpipeline.IncludeEntry) {
	t.Helper()

	coordinate := strings.TrimPrefix(include.Component, "catalog.example/components/")

	component, _, ok := strings.Cut(coordinate, "@")
	if !ok || component == "" {
		t.Fatalf("invalid generated component coordinate %q", include.Component)
	}

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), "templates", component+".yml")) //nolint:gosec // checked-in contract fixture.
	if err != nil {
		t.Fatalf("generated component %q has no catalogue template: %v", component, err)
	}

	var document struct {
		Spec struct {
			Inputs map[string]struct {
				Type string `yaml:"type"`
			} `yaml:"inputs"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("parse %s component contract: %v", component, err)
	}

	for name, value := range include.Inputs {
		input, exists := document.Spec.Inputs[name]
		if !exists {
			t.Errorf("%s generated undeclared input %q", component, name)

			continue
		}

		switch input.Type {
		case "", "string":
			if _, ok := value.(string); !ok {
				t.Errorf("%s input %q = %#v, want string", component, name, value)
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				t.Errorf("%s input %q = %#v, want boolean", component, name, value)
			}
		default:
			t.Errorf("%s generated input %q with unsupported catalogue type %q", component, name, input.Type)
		}
	}
}
