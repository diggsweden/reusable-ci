// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeCargoTool struct {
	version string
	err     error
	calls   *int
}

func (f fakeCargoTool) Version(context.Context) (string, error) {
	if f.calls != nil {
		*f.calls++
	}

	return f.version, f.err
}

func TestCargoPrerequisites_UsesCargoArtifactWorkingDirectory(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.Chdir()

	var out, stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, &out, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"crates/api"}]}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "Cargo.lock present in crates/api") {
		t.Errorf("stderr = %s", stderr.String())
	}

	if !strings.Contains(out.String(), "cargo 1.90.0") {
		t.Errorf("out = %s", out.String())
	}
}

func TestCargoPrerequisites_FailsWhenCargoLockMissingInWorkingDirectory(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.MkdirAll("crates/api")
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]\nchannel='1.90.0'\n"))
	fsys.Chdir()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"crates/api"}]}}}`,
	})
	// The pipeline stops on a domain-rule violation, not a broken flag.
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(stderr.String(), "Cargo.lock not found in crates/api") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestCargoPrerequisites_SkipsWhenTargetDoesNotRun(t *testing.T) {
	t.Parallel()

	var out, stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{err: errors.New("should not run")}, &out, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ //nolint:err113 // test mock error
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":false,"items":[{"name":"api","project_type":"cargo","working_directory":"missing"}]}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "No Cargo artifacts") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

// TestCargoPrerequisites_FailsWhenToolchainPinMissing pins the
// compiler-consistency requirement: a Cargo artifact without
// rust-toolchain.toml (or its legacy plain-text sibling) fails the pipeline.
// The pin supports reproducible builds but does not guarantee identical output.
func TestCargoPrerequisites_FailsWhenToolchainPinMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	// No rust-toolchain.toml — the missing pin is the violation.
	fsys.Chdir()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"crates/api"}]}}}`,
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(stderr.String(), "::error::No rust-toolchain.toml") {
		t.Errorf("stderr should carry an error annotation:\n%s", stderr.String())
	}
}

// TestCargoPrerequisites_RejectsUnsafeWorkingDirectory covers both refusals the
// shared working-directory guard makes. The validator walks the directory a
// plan names, so one that escapes or is absolute takes it outside the
// workspace; only the escaping case had a row, leaving the absolute branch
// untested by any caller.
func TestCargoPrerequisites_RejectsUnsafeWorkingDirectory(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct{ dir, want string }{
		"climbs out of the workspace": {dir: "../outside", want: "escapes the workspace"},
		"is absolute":                 {dir: "/etc", want: "must be relative"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			plan := `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"` + testCase.dir + `"}]}}}`

			err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
				PublishStagePlanJSON: plan,
			})
			if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want an invalid-config error mentioning %q", err, testCase.want)
			}
		})
	}
}

// TestCargoPrerequisites_CoversBothBuildModesViaConfigPlan exercises the
// config-plan path so artifact-first cargo (which never shows up in the
// publish-stage projection) still gets its Cargo.lock + toolchain
// validated. The historical bug this guards against: artifact-first
// cargo silently skipping prerequisite checks because only the publish
// projection was wired in.
func TestCargoPrerequisites_CoversBothBuildModesViaConfigPlan(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.WriteFile("crates/cli/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/cli/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.Chdir()

	var out, stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, &out, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		// The config-plan-json carries every Cargo artifact regardless of
		// build-mode. `api` is container-first, `cli` is artifact-first.
		ConfigPlanJSON: validationPlanJSON(t, validationConfigPlan(t,
			config.Artifact{Name: "api", ProjectType: projecttype.Cargo, WorkingDirectory: "crates/api", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
			config.Artifact{Name: "cli", ProjectType: projecttype.Cargo, WorkingDirectory: "crates/cli", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst}})),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"Cargo.lock present in crates/api", "Cargo.lock present in crates/cli"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("missing %q in stderr:\n%s", want, stderr.String())
		}
	}
}
