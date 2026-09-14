// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"encoding/json"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

type configPlanWriter struct {
	buf      bytes.Buffer
	attempts int
}

func (w *configPlanWriter) Write(body []byte) (int, error) {
	w.attempts++

	return w.buf.Write(body)
}

func (w *configPlanWriter) String() string { return w.buf.String() }

func validationConfigPlan(t *testing.T, artifacts ...config.Artifact) pipeline.ConfigPlan {
	t.Helper()

	cfg := config.Config{Artifacts: artifacts}
	require.NoError(t, config.Derive(&cfg))
	plan := pipeline.NewConfigPlan(&cfg)
	require.NoError(t, pipeline.ValidateConfigPlan(plan))

	return plan
}

func validationPlanJSON(t *testing.T, plan pipeline.ConfigPlan) string {
	t.Helper()

	body, err := json.Marshal(plan)
	require.NoError(t, err)

	return string(body)
}

func TestCargoPrerequisites_ConfigPlanProjectionCannotBypassChecks(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.MkdirAll("crates/api")
	fsys.Chdir()

	plan := validationConfigPlan(t, config.Artifact{Name: "api", ProjectType: projecttype.Cargo, WorkingDirectory: "crates/api"})
	require.Len(t, plan.Artifacts.All, 1)
	require.Len(t, plan.Artifacts.Cargo, 1)

	var out, annot configPlanWriter

	calls := 0
	tool := fakeCargoTool{version: "cargo 1.90.0", calls: &calls}
	in := appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: validationPlanJSON(t, plan)}
	err := appvalidate.CargoPrerequisites(t.Context(), tool, &out, output.NewAnnotator(&annot, output.FormatGitHub), in)
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Contains(t, annot.String(), "Cargo.lock not found in crates/api")
	require.Contains(t, annot.String(), "No rust-toolchain.toml or rust-toolchain pin found in crates/api")
	require.Equal(t, 1, calls)
	require.Equal(t, "cargo 1.90.0\n", out.String())

	// Change only the redundant Cargo projection, not All or either build-mode list.
	plan.Artifacts.Cargo = []pipeline.PlannedArtifact{}

	for _, seed := range []string{"", "existing channel\n"} {
		out, annot = configPlanWriter{}, configPlanWriter{}
		_, _ = out.buf.WriteString(seed)
		_, _ = annot.buf.WriteString(seed)
		calls = 0
		in.ConfigPlanJSON = validationPlanJSON(t, plan)
		in.PublishStagePlanJSON = `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":false}}}`
		err = appvalidate.CargoPrerequisites(t.Context(), tool, &out, output.NewAnnotator(&annot, output.FormatGitHub), in)
		require.ErrorIs(t, err, errs.ErrInvalidConfig)
		require.Contains(t, err.Error(), "config-plan artifacts.cargo disagrees with artifacts.all")
		require.Zero(t, calls)
		require.Zero(t, out.attempts)
		require.Zero(t, annot.attempts)
		require.Equal(t, seed, out.String())
		require.Equal(t, seed, annot.String())
	}
}

func TestPrerequisiteConsumers_ConfigPlanValidatesLaterArtifactBeforeEffects(t *testing.T) {
	for _, kind := range []projecttype.Type{projecttype.Cargo, projecttype.Maven} {
		t.Run(string(kind), func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			for _, dir := range []string{"first", "later"} {
				fsys.WriteFile(dir+"/Cargo.lock", []byte("lock literal\n"))
				fsys.WriteFile(dir+"/rust-toolchain", []byte("1.90.0\n"))
				fsys.WriteFile(dir+"/pom.xml", []byte(`<project><properties><project.build.outputTimestamp>literal-time</project.build.outputTimestamp></properties></project>`))
			}

			for _, tc := range []struct {
				seed string
				bad  bool
			}{
				{}, {bad: true}, {seed: "existing channel\n"}, {seed: "existing channel\n", bad: true},
			} {
				seed, bad := tc.seed, tc.bad

				plan := validationConfigPlan(t,
					config.Artifact{Name: "first", ProjectType: kind, WorkingDirectory: "first"},
					config.Artifact{Name: "later", ProjectType: kind, WorkingDirectory: "later"})
				if bad {
					plan.Artifacts.All[1].ProjectType = "unsupported"
				}

				var out, annot configPlanWriter

				_, _ = out.buf.WriteString(seed)
				_, _ = annot.buf.WriteString(seed)
				calls := 0

				var err error
				if kind == projecttype.Cargo {
					err = appvalidate.CargoPrerequisites(t.Context(), fakeCargoTool{version: "cargo literal", calls: &calls}, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: validationPlanJSON(t, plan), PublishStagePlanJSON: "invalid unused fallback"})
				} else {
					err = appvalidate.JVMReproducibility(t.Context(), &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.JVMReproducibilityInput{ConfigPlanJSON: validationPlanJSON(t, plan)})
				}

				if bad {
					require.ErrorIs(t, err, errs.ErrInvalidConfig)
					require.Contains(t, err.Error(), "config-plan artifacts.all[1] has an unsupported project_type")
					require.Zero(t, calls)
					require.Zero(t, out.attempts)
					require.Zero(t, annot.attempts)
					require.Equal(t, seed, out.String())
					require.Equal(t, seed, annot.String())

					continue
				}

				require.NoError(t, err)
				require.Positive(t, out.attempts)
				require.Positive(t, annot.attempts)

				if kind == projecttype.Cargo {
					require.Equal(t, 1, calls)
					require.Equal(t, seed+"cargo literal\n", out.String())
					require.Contains(t, annot.String(), "Cargo.lock present in first")
					require.Contains(t, annot.String(), "Cargo.lock present in later")
				} else {
					require.Contains(t, out.String(), "first: project.build.outputTimestamp=literal-time\n")
					require.Contains(t, out.String(), "later: project.build.outputTimestamp=literal-time\n")
				}
			}
		})
	}
}

func TestPrerequisiteConsumers_ConfigPlanWorkingDirectoryDefaults(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("Cargo.lock", []byte("lock"))
	fsys.WriteFile("rust-toolchain", []byte("1.90.0"))
	fsys.WriteFile("pom.xml", []byte(`<project><properties><project.build.outputTimestamp>root-time</project.build.outputTimestamp></properties></project>`))

	for _, dir := range []string{".", ""} {
		plan := validationConfigPlan(t,
			config.Artifact{Name: "crate", ProjectType: projecttype.Cargo},
			config.Artifact{Name: "jvm", ProjectType: projecttype.Maven})
		// Empty working directories remain supported when every copy agrees.
		plan.Artifacts.All[0].WorkingDirectory = dir
		plan.Artifacts.Cargo[0].WorkingDirectory = dir
		plan.Artifacts.CargoArtifactFirst[0].WorkingDirectory = dir
		plan.Artifacts.All[1].WorkingDirectory = dir
		plan.Artifacts.Maven[0].WorkingDirectory = dir
		require.NoError(t, pipeline.ValidateConfigPlan(plan))

		var out, annot bytes.Buffer

		calls := 0
		require.NoError(t, appvalidate.CargoPrerequisites(t.Context(), fakeCargoTool{version: "cargo root", calls: &calls}, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: validationPlanJSON(t, plan)}))
		require.Equal(t, 1, calls)
		require.Equal(t, "cargo root\n", out.String())
		require.NoError(t, appvalidate.JVMReproducibility(t.Context(), &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.JVMReproducibilityInput{ConfigPlanJSON: validationPlanJSON(t, plan)}))
		require.Contains(t, out.String(), "project.build.outputTimestamp=root-time\n")
	}
}

func TestCargoPrerequisites_ConfigPlanSelectionAndPublishFallback(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("selected/Cargo.lock", []byte("lock"))
	fsys.WriteFile("selected/rust-toolchain", []byte("1.90.0"))

	const fallback = `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"crate","project_type":"cargo","working_directory":"selected"}]}}}`
	for _, tc := range []struct {
		name, canonical, fallback, reason string
		calls                             int
	}{
		{name: "absent canonical", fallback: fallback, calls: 1},
		{name: "whitespace canonical", canonical: " \n", fallback: fallback, calls: 1},
		{name: "runs false ignores items", fallback: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":false,"items":[{"working_directory":"missing"}]}}}`},
		{name: "empty canonical wins", canonical: validationPlanJSON(t, validationConfigPlan(t)), fallback: fallback},
		{name: "invalid selected JSON", canonical: "not JSON", fallback: fallback, reason: "parse config-plan-json:"},
		{name: "invalid selected version", canonical: `{"version":999}`, fallback: fallback, reason: "config-plan-json has unsupported version 999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, annot configPlanWriter

			calls := 0
			err := appvalidate.CargoPrerequisites(t.Context(), fakeCargoTool{version: "cargo selected", calls: &calls}, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: tc.canonical, PublishStagePlanJSON: tc.fallback})
			require.Equal(t, tc.calls, calls)

			if tc.reason != "" {
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				require.Contains(t, err.Error(), tc.reason)
				require.Zero(t, out.attempts)
				require.Zero(t, annot.attempts)

				return
			}

			require.NoError(t, err)

			if tc.calls == 0 {
				require.Zero(t, out.attempts)
				require.Equal(t, "::notice::No Cargo artifacts\n", annot.String())
			} else {
				require.Equal(t, "cargo selected\n", out.String())
				require.Contains(t, annot.String(), "Cargo.lock present in selected")
				require.Contains(t, annot.String(), "Toolchain pin present in selected")
			}
		})
	}
}
