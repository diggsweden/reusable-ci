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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeCargoTool struct {
	version string
	err     error
}

func (f fakeCargoTool) Version(context.Context) (string, error) {
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
	fsys.Chdir()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"crates/api"}]}}}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "Cargo.lock not found in crates/api") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestCargoPrerequisites_FailsWhenCargoMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("Cargo.lock", []byte("# lock"))
	fsys.Chdir()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{err: errors.New("not found")}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{ //nolint:err113 // test mock error
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"."}]}}}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "cargo is not installed") {
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

	if !strings.Contains(stderr.String(), "No Cargo artefacts") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

// TestCargoPrerequisites_FailsWhenToolchainPinMissing pins the
// deterministic-pipeline guarantee: a Cargo artefact without
// rust-toolchain.toml (or its legacy plain-text sibling) fails the
// pipeline. The pinned toolchain is the only way CI and local-dev
// builds produce byte-identical Rust binaries.
func TestCargoPrerequisites_FailsWhenToolchainPinMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	// No rust-toolchain.toml — the missing pin is the violation.
	fsys.Chdir()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"crates/api"}]}}}`,
	})
	if err == nil {
		t.Fatal("expected error when rust-toolchain pin is missing")
	}

	if !strings.Contains(stderr.String(), "::error::No rust-toolchain.toml") {
		t.Errorf("stderr should carry an error annotation:\n%s", stderr.String())
	}
}

func TestCargoPrerequisites_RejectsEscapingWorkingDirectory(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0"}, nil, output.NewAnnotator(&stderr, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"../outside"}]}}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
		t.Fatalf("err = %v", err)
	}
}

// TestCargoPrerequisites_CoversBothBuildModesViaConfigPlan exercises the
// config-plan path so artefact-first cargo (which never shows up in the
// publish-stage projection) still gets its Cargo.lock + toolchain
// validated. The historical bug this guards against: artefact-first
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
		// The config-plan-json carries every Cargo artefact regardless of
		// build-mode. `api` is container-first, `cli` is artefact-first.
		ConfigPlanJSON: `{"version":1,"artifacts":{"cargo":[
			{"name":"api","project_type":"cargo","working_directory":"crates/api","cargo_build_mode":"container-first"},
			{"name":"cli","project_type":"cargo","working_directory":"crates/cli","cargo_build_mode":"artifact-first"}
		]}}`,
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
