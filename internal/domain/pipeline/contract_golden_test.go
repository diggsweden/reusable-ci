// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/testutil/golden"
)

func TestPlanContracts_Golden(t *testing.T) {
	t.Parallel()
	cfg := contractConfig(t)
	configPlan := pipeline.NewConfigPlan(cfg)

	releasePlan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:           configPlan,
		Branch:               "main",   //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefName:              "v1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleasePublisher:     "github-cli",
		ReleaseSBOMs:         "build,analyzed-container",
		ReleaseSignArtifacts: true,
		ChangelogCreator:     "git-cliff",
	})
	if err != nil {
		t.Fatal(err)
	}

	devPlan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:          configPlan,
		Branch:              "feature/demo",
		ReleaseSHA:          "abc1234",
		ReleaseActor:        "octocat",
		ReleaseRepository:   "org/repo",
		Registry:            "ghcr.io",
		ReusableCIBinaryRef: "v3.0.0",
		NPMRegistry:         "https://npm.pkg.github.com",
		PackageScope:        "@org",
		SBOMs:               "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishNPM:          true,
		UseCIToken:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	prPlan := pipeline.NewPRPlan(pipeline.PRPlanInput{
		ProjectType:         projecttype.Go,
		BaseBranch:          "main",
		ReusableCIBinaryRef: "v3.0.0",
		Nanolinter:          true,
	})

	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "config-plan.json", value: configPlan},
		{name: "release-plan.json", value: releasePlan},
		{name: "snapshot-release-plan.json", value: devPlan},
		{name: "pr-plan.json", value: prPlan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.MarshalIndent(tc.value, "", "  ")
			if err != nil {
				t.Fatal(err)
			}

			golden.Equal(t, tc.name, append(body, '\n'))
		})
	}
}

func contractConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name:                 "lib", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				ProjectType:          projecttype.Maven,
				WorkingDirectory:     "services/lib",
				BuildType:            config.BuildTypeLibrary,
				PublishTo:            []config.PublishTarget{config.PublishMavenCentral},
				SBOMs:                "all",
				RequireAuthorization: true,
				Maven: &config.MavenConfig{
					JavaVersion:  "25",
					SettingsPath: "settings.xml",
				},
			},
			{
				Name:             "web", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				ProjectType:      projecttype.NPM,
				WorkingDirectory: "apps/web",
				PublishTo:        []config.PublishTarget{config.PublishGitHubPackages, config.PublishNPMJS},
				NPM: &config.NPMConfig{
					NodeVersion: "24",
				},
			},
			{
				Name:             "api", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				ProjectType:      projecttype.Go,
				WorkingDirectory: "cmd/api",
				Go: &config.GoConfig{
					BuildMode: config.GoBuildModeContainerFirst,
				},
			},
		},
		Containers: []config.Container{
			{
				Name:      "api",
				From:      []string{"api"},
				Platforms: "linux/amd64,linux/arm64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				BuildArgs: map[string]string{"VERSION": "${VERSION}"},
				Extract: &config.ContainerExtract{Binary: &config.ContainerExtractBinary{
					Target: "export", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
					Names:  []string{"api"},
				}},
			},
		},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	return cfg
}
