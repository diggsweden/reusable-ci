// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func TestConfigPlanInvariant_ProjectionMembership(t *testing.T) {
	t.Parallel()

	for _, group := range []struct{ field, wire string }{
		{"Maven", "maven"}, {"NPM", "npm"}, {"Gradle", "gradle"},
		{"GradleAndroid", "gradle_android"}, {"XcodeIOS", "xcode_ios"},
		{"Python", "python"}, {"Go", "go"}, {"Cargo", "cargo"}, {"Meta", "meta"},
		{"GoArtifactFirst", "go_artifact_first"}, {"GoContainerFirst", "go_container_first"},
		{"CargoArtifactFirst", "cargo_artifact_first"}, {"CargoContainerFirst", "cargo_container_first"},
		{"ForgePackages", "forge_packages"}, {"MavenCentral", "maven_central"},
		{"GooglePlay", "google_play"}, {"NPMJS", "npmjs"},
	} {
		for _, fault := range []string{"extra", "missing", "duplicate", "copy"} {
			if (group.field == "Python" || group.field == "NPMJS") && fault != "extra" {
				continue // Unsupported groups have no legitimate member to remove or duplicate.
			}

			t.Run(group.wire+"/"+fault, func(t *testing.T) {
				t.Parallel()
				plan := invariantConfigPlan(t)
				field := reflect.ValueOf(&plan.Artifacts).Elem().FieldByName(group.field)

				items, ok := field.Interface().([]pipeline.PlannedArtifact)
				if !ok || len(items) == 0 && fault != "extra" {
					t.Fatalf("invalid group fixture %s", group.field)
				}

				switch fault {
				case "extra":
					items = append(items, pipeline.PlannedArtifact{Name: "foreign", ProjectType: projecttype.NPM, WorkingDirectory: "."})
				case "missing":
					items = items[:len(items)-1]
				case "duplicate":
					items = append(items, items[len(items)-1])
				case "copy":
					items[len(items)-1].WorkingDirectory = "other-project"
				}

				field.Set(reflect.ValueOf(items))
				assertInvariantRefusal(t, plan, "artifacts."+group.wire+" disagrees")
			})
		}
	}
}

func TestConfigPlanInvariant_Contradictions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, reason string
		mutate       func(*pipeline.ConfigPlan)
	}{
		{"old unsupported version", "unsupported config-plan version", func(p *pipeline.ConfigPlan) { p.Version = 0 }},
		{"future version", "unsupported config-plan version", func(p *pipeline.ConfigPlan) { p.Version = 2 }},
		{"duplicate all", "artifacts.all[11]", func(p *pipeline.ConfigPlan) {
			p.Artifacts.All = append(p.Artifacts.All, p.Artifacts.All[10])
			p.Artifacts.Meta = append(p.Artifacts.Meta, p.Artifacts.Meta[0])
		}},
		{"empty artifact name", "artifacts.all[10]", func(p *pipeline.ConfigPlan) {
			p.Artifacts.All[10].Name = ""
			p.Artifacts.Meta[0].Name = ""
		}},
		{"unknown type", "unsupported project_type", func(p *pipeline.ConfigPlan) {
			p.Artifacts.All[10].ProjectType = "unknown"
			p.Artifacts.Meta = nil
		}},
		{"go mode", "inconsistent build modes", func(p *pipeline.ConfigPlan) { p.Artifacts.All[6].GoBuildMode = config.GoBuildModeContainerFirst }},
		{"cargo mode", "inconsistent build modes", func(p *pipeline.ConfigPlan) { p.Artifacts.All[8].CargoBuildMode = config.CargoBuildModeContainerFirst }},
		{"authorization aggregate", "any_require_authorization", func(p *pipeline.ConfigPlan) { p.AnyRequireAuthorization = false }},
		{"sbom aggregate", "pipeline_sboms disagrees", func(p *pipeline.ConfigPlan) { p.PipelineSBOMs = "none" }},
		{"fallback", "fallback_project_type", func(p *pipeline.ConfigPlan) { p.FallbackProjectType = projecttype.Go }},
		{"container gate", "containers.has_containers", func(p *pipeline.ConfigPlan) { p.Containers.HasContainers = false }},
		{"duplicate container", "containers.all[2]", func(p *pipeline.ConfigPlan) { p.Containers.All = append(p.Containers.All, p.Containers.All[0]) }},
		{"empty container name", "containers.all[1]", func(p *pipeline.ConfigPlan) { p.Containers.All[1].Name = "" }},
		{"reordered group", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) {
			p.Artifacts.Maven[0], p.Artifacts.Maven[1] = p.Artifacts.Maven[1], p.Artifacts.Maven[0]
		}},
		{"copy authorization", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.Maven[1].RequireAuthorization = false }},
		{"copy publish target", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.Maven[1].PublishTo = nil }},
		{"copy transfer name", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.Maven[1].BuildArtifactName = "other-upload" }},
		{"copy sbom layers", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.Maven[1].EffectiveSBOMs = nil }},
		{"nested execution config", "artifacts.npm disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.NPM[0].NPM.NodeVersion = "99" }},
		{"id token", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.RequiresIDToken = false }},
		{"gpg import", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.ImportsGPGKey = true }},
		{"container signing", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.SignsContainers = false }},
		{"egress", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.RequiresSigstoreEgress = false }},
		{"unresolved transparency", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.Transparency = "" }},
		{"unknown signing", "sign", func(p *pipeline.ConfigPlan) { p.Sign.Method = "unknown" }},
		{"git gpg import", "git_signing has inconsistent", func(p *pipeline.ConfigPlan) { p.GitSigning.ImportsGPGKey = true }},
		{"unknown git signing", "git-signing.method", func(p *pipeline.ConfigPlan) { p.GitSigning.Method = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := invariantConfigPlan(t)
			tc.mutate(&plan)
			assertInvariantRefusal(t, plan, tc.reason)
		})
	}
}

func TestConfigPlanInvariant_ProducerShapes(t *testing.T) {
	t.Parallel()

	plan := invariantConfigPlan(t)
	if plan.Version != 1 {
		t.Fatalf("wire version = %d, want the supported v1 contract", plan.Version)
	}
	// Maven applications may request forge-packages but are deliberately excluded.
	if got := names(plan.Artifacts.ForgePackages); !reflect.DeepEqual(got, []string{"web", "lib"}) {
		t.Fatalf("forge packages = %v", got)
	}

	if plan.Artifacts.Go[0].Go != nil || plan.Artifacts.Cargo[0].Cargo != nil {
		t.Fatal("fixture must exercise omitted native config blocks and default modes")
	}

	plan.Artifacts.Python = nil
	plan.Artifacts.NPMJS = nil
	plan.Artifacts.NPM[0].PublishTo = []config.PublishTarget{config.PublishForgePackages}
	plan.Artifacts.Meta[0].EffectiveSBOMs = []config.SBOMLayer{}

	plan.PipelineSBOMs = "analyzed-container,build,analyzed-artifact,build"
	if err := pipeline.ValidateConfigPlan(plan); err != nil {
		t.Fatal(err)
	}

	release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSignArtifacts: true, ReleaseSBOMs: "all"})
	if err != nil || !release.Policy.RequireAllowlistedSigner || !release.Stages.Publish.Targets.Containers.Runs {
		t.Fatalf("release control = %+v, %v", release, err)
	}

	if release.Stages.Prepare.Targets.VersionBump.Runs || len(release.Stages.Prepare.Targets.VersionBump.Items) != 11 {
		t.Fatal("disabled version bump must retain all items")
	}

	snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "none"})
	if err != nil || snapshot.Context.ProjectType != projecttype.Maven {
		t.Fatalf("explicit snapshot override = %+v, %v", snapshot.Context, err)
	}

	for _, target := range []pipeline.TargetPlan[pipeline.PlannedArtifact]{snapshot.Stages.Publish.Targets.NPM, snapshot.Stages.Publish.Targets.GoArtifactFirst, snapshot.Stages.Publish.Targets.CargoArtifactFirst, snapshot.Stages.Publish.Targets.GoContainerFirst} {
		if target.Runs || len(target.Items) != 1 {
			t.Fatalf("disabled support target must retain its item: %+v", target)
		}
	}
}

func TestConfigPlanInvariant_SigningControls(t *testing.T) {
	t.Parallel()

	for _, sign := range []config.SignConfig{
		{}, {Method: domainrelease.SignMethodGPG},
		{Method: domainrelease.SignMethodSigstore, OIDCIssuer: "https://issuer.example.test"},
		{Method: domainrelease.SignMethodKMS, Key: "hashivault://transit/keys/release"},
		{Method: domainrelease.SignMethodKMS, Key: "file:fixture.key", Transparency: domainrelease.TransparencyNone},
	} {
		for _, method := range []config.GitSignMethod{"", config.GitSignGPG, config.GitSignSSH} {
			cfg := &config.Config{Sign: sign, GitSigning: config.GitSigningConfig{Method: method}, Artifacts: []config.Artifact{{Name: "meta", ProjectType: projecttype.Meta}}}
			if err := config.Validate(cfg); err != nil {
				t.Fatal(err)
			}

			if err := config.Derive(cfg); err != nil {
				t.Fatal(err)
			}

			plan := pipeline.NewConfigPlan(cfg)
			if err := pipeline.ValidateConfigPlan(plan); err != nil {
				t.Fatalf("sign=%+v git=%s: %v", sign, method, err)
			}

			if _, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "none"}); err != nil {
				t.Fatal(err)
			}

			plan.Sign.SignsContainers = !plan.Sign.SignsContainers
			assertInvariantRefusal(t, plan, "sign has inconsistent")
		}
	}
}

func assertInvariantRefusal(t *testing.T, plan pipeline.ConfigPlan, reason string) {
	t.Helper()

	before, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	if err = pipeline.ValidateConfigPlan(plan); !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), reason) {
		t.Errorf("ValidateConfigPlan = %v, want %s / ErrInvalidConfig", err, reason)
	}

	release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSignArtifacts: true, ReleaseSBOMs: "all"})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), reason) || !reflect.DeepEqual(release, pipeline.ReleasePlan{}) {
		t.Errorf("release refusal = %+v, %v", release, err)
	}

	snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "none"})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), reason) || !reflect.DeepEqual(snapshot, pipeline.SnapshotReleasePlan{}) {
		t.Errorf("snapshot refusal = %+v, %v", snapshot, err)
	}

	after, err := json.Marshal(plan)
	if err != nil || string(before) != string(after) {
		t.Fatalf("refusal mutated input: %v", err)
	}
}

func invariantConfigPlan(t *testing.T) pipeline.ConfigPlan {
	t.Helper()

	no := false

	cfg := &config.Config{
		Sign: config.SignConfig{Method: domainrelease.SignMethodSigstore}, GitSigning: config.GitSigningConfig{Method: config.GitSignSSH},
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishForgePackages}, NPM: &config.NPMConfig{NodeVersion: "24"}},
			{Name: "app", ProjectType: projecttype.Maven, BuildType: config.BuildTypeApplication, PublishTo: []config.PublishTarget{config.PublishForgePackages}},
			{Name: "lib", ProjectType: projecttype.Maven, BuildType: config.BuildTypeLibrary, PublishTo: []config.PublishTarget{config.PublishForgePackages, config.PublishMavenCentral}, RequireAuthorization: true},
			{Name: "jvm", ProjectType: projecttype.Gradle},
			{Name: "android", ProjectType: projecttype.GradleAndroid, PublishTo: []config.PublishTarget{config.PublishGooglePlay}, GradleAndroid: &config.GradleAndroidConfig{BuildTypes: "debug", IncludeAAB: &no}},
			{Name: "ios", ProjectType: projecttype.XcodeIOS, XcodeIOS: &config.XcodeIOSConfig{EnableCodeSigning: &no}},
			{Name: "go-cli", ProjectType: projecttype.Go},
			{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
			{Name: "cargo-cli", ProjectType: projecttype.Cargo},
			{Name: "cargo-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
			{Name: "meta", ProjectType: projecttype.Meta},
		},
		Containers: []config.Container{{Name: "web", From: []string{"web"}}, {Name: "bare", EnableSLSA: &no, EnableScan: &no}},
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatal(err)
	}

	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}
	// A process boundary detaches nested pointers shared by the producer's copies.
	body, err := json.Marshal(pipeline.NewConfigPlan(cfg))
	if err != nil {
		t.Fatal(err)
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}

	if err := pipeline.ValidateConfigPlan(plan); err != nil {
		t.Fatalf("producer control: %v", err)
	}

	return plan
}
