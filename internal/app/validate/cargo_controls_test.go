// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// publishPlan is a legacy publish-stage plan whose container-first Cargo
// target runs over the given working directories.
func publishPlan(dirs ...string) string {
	items := make([]string, 0, len(dirs))
	for i, dir := range dirs {
		items = append(items, fmt.Sprintf(`{"name":"crate%d","project_type":"cargo","working_directory":%q}`, i, dir))
	}

	return `{"version":1,"stage":"publish","targets":{"cargo_container_first":{"runs":true,"items":[` + strings.Join(items, ",") + `]}}}`
}

// TestCargoPrerequisites_RefusedInputsTouchNothing covers the inputs refused
// before any directory is inspected: a legacy plan that is not exactly the
// publish-stage contract, and an unsafe directory after a valid one. Each is
// refused with no annotation, no version output and no cargo call. A misspelt
// target used to decode as "not running" and pass with "No Cargo artifacts",
// and an unsafe later directory used to arrive after the earlier directory had
// already been reported.
func TestCargoPrerequisites_RefusedInputsTouchNothing(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.Chdir()

	for _, tc := range []struct {
		name string
		in   appvalidate.CargoPrerequisitesInput
		want error
	}{
		{"no plan", appvalidate.CargoPrerequisitesInput{}, errs.ErrUsage},
		{"misspelt target", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: `{"version":1,"stage":"publish","targets":{"cargo_containers_first":{"runs":true,"items":[{"name":"api","project_type":"cargo","working_directory":"missing"}]}}}`}, errs.ErrInvalidConfig},
		{"trailing value", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: publishPlan("crates/api") + `{}`}, errs.ErrInvalidConfig},
		{"not json", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: `{"version":`}, errs.ErrInvalidConfig},
		{"wrong version", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: `{"version":2,"stage":"publish","targets":{}}`}, errs.ErrInvalidConfig},
		{"wrong stage", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: `{"version":1,"stage":"build","targets":{}}`}, errs.ErrInvalidConfig},
		{"later directory escapes", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: publishPlan("crates/api", "../outside")}, errs.ErrInvalidConfig},
		{"later directory absolute", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: publishPlan("crates/api", "/etc")}, errs.ErrInvalidConfig},
		{"later directory missing", appvalidate.CargoPrerequisitesInput{PublishStagePlanJSON: publishPlan("crates/api", "crates/gone")}, errs.ErrInvalidConfig},
		{"invalid config plan wins over a valid fallback", appvalidate.CargoPrerequisitesInput{ConfigPlanJSON: `{"version":0}`, PublishStagePlanJSON: publishPlan("crates/api")}, errs.ErrInvalidConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, annot bytes.Buffer

			calls := 0

			err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0", calls: &calls}, &out, output.NewAnnotator(&annot, output.FormatGitHub), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if calls != 0 || out.Len() != 0 || annot.Len() != 0 {
				t.Errorf("refused input had effects: calls=%d out=%q annotations=%q", calls, out.String(), annot.String())
			}
		})
	}
}

// TestCargoPrerequisites_EachDirectoryIsReportedOnceInPlanOrder spells the same
// directory three ways and puts a second directory between them: each directory
// is inspected once, in the order it first appears, and cargo runs once.
func TestCargoPrerequisites_EachDirectoryIsReportedOnceInPlanOrder(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/api/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/api/rust-toolchain.toml", []byte("[toolchain]"))
	fsys.WriteFile("crates/cli/Cargo.lock", []byte("# lock"))
	fsys.WriteFile("crates/cli/rust-toolchain", []byte("1.90.0"))
	fsys.Chdir()

	var out, annot bytes.Buffer

	calls := 0

	err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{version: "cargo 1.90.0", calls: &calls}, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
		PublishStagePlanJSON: publishPlan("crates/cli/", "crates/api", "./crates/cli", "crates/api/../cli"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "::notice::Cargo.lock present in crates/cli\n" +
		"::notice::Toolchain pin present in crates/cli\n" +
		"::notice::Cargo.lock present in crates/api\n" +
		"::notice::Toolchain pin present in crates/api\n"
	if annot.String() != want {
		t.Errorf("annotations:\n%s\nwant:\n%s", annot.String(), want)
	}

	if calls != 1 || out.String() != "cargo 1.90.0\n" {
		t.Errorf("calls=%d out=%q, want one cargo call and its version", calls, out.String())
	}
}

// TestCargoPrerequisites_FailureClassesFollowTheirCause pins which class a
// failed run carries. Missing project files are invalid configuration; cargo
// failing to report a version keeps the class it failed with, so a runner
// without cargo is unavailable rather than a broken project; both together
// carry both. Every check still runs, so one run reports everything wrong.
func TestCargoPrerequisites_FailureClassesFollowTheirCause(t *testing.T) {
	unavailable := fmt.Errorf("cargo not found in $PATH: %w", errs.ErrDependencyUnavailable)

	for _, tc := range []struct {
		name        string
		lock        bool
		cargoErr    error
		wantInvalid bool
		wantCode    errs.ExitCodeType
	}{
		{name: "cargo unavailable", lock: true, cargoErr: unavailable, wantCode: errs.ExitCodeUnavailable},
		{name: "cargo cancelled", lock: true, cargoErr: context.Canceled, wantCode: errs.ExitCodeUsage},
		{name: "lock missing", wantInvalid: true, wantCode: errs.ExitCodeConfiguration},
		{name: "lock missing and cargo unavailable", cargoErr: unavailable, wantInvalid: true, wantCode: errs.ExitCodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("rust-toolchain.toml", []byte("[toolchain]"))

			if tc.lock {
				fsys.WriteFile("Cargo.lock", []byte("# lock"))
			}

			fsys.Chdir()

			var annot bytes.Buffer

			calls := 0

			err := appvalidate.CargoPrerequisites(context.Background(), fakeCargoTool{err: tc.cargoErr, calls: &calls}, nil, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.CargoPrerequisitesInput{
				PublishStagePlanJSON: publishPlan("."),
			})
			if err == nil {
				t.Fatal("err = nil, want a failure")
			}

			if errors.Is(err, errs.ErrInvalidConfig) != tc.wantInvalid {
				t.Errorf("ErrInvalidConfig = %t, want %t: %v", !tc.wantInvalid, tc.wantInvalid, err)
			}

			if tc.cargoErr != nil && !errors.Is(err, tc.cargoErr) {
				t.Errorf("err = %v, want it to carry the cargo failure", err)
			}

			if got := errs.ExitCodeFromError(err); got != tc.wantCode {
				t.Errorf("exit code = %d, want %d", got, tc.wantCode)
			}

			if calls != 1 {
				t.Errorf("cargo called %d times, want 1", calls)
			}

			if !tc.lock && !bytes.Contains(annot.Bytes(), []byte("::error::Cargo.lock not found in repo root\n")) {
				t.Errorf("missing lock not reported: %q", annot.String())
			}

			if tc.cargoErr != nil && !bytes.Contains(annot.Bytes(), []byte("::error::cargo is not usable on the runner: "+tc.cargoErr.Error()+"\n")) {
				t.Errorf("cargo failure not reported: %q", annot.String())
			}
		})
	}
}
