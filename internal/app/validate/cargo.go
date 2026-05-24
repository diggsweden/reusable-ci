// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
)

// CargoTool abstracts the local cargo executable for tests.
type CargoTool interface {
	Version(ctx context.Context) (string, error)
}

// CargoPrerequisitesInput drives `validate cargo`. Either input is
// accepted; ConfigPlanJSON is the canonical source (covers cargo
// artefacts in both build-modes), and PublishStagePlanJSON is kept for
// backward compatibility with direct callers that only have the
// publish-stage projection.
type CargoPrerequisitesInput struct {
	ConfigPlanJSON       string
	PublishStagePlanJSON string
}

// CargoPrerequisites verifies lockfile/toolchain state for every planned
// Cargo artefact (artefact-first AND container-first) in its own working
// directory. The reproducibility invariants (committed Cargo.lock, pinned
// rust-toolchain) apply equally to both build-modes — artefact-first
// crates that ship as standalone binaries get the same scrutiny as
// container-first ones embedded in a runtime image.
//
//nolint:cyclop // validates each required Cargo manifest field + Cargo.lock invariant.
func CargoPrerequisites(ctx context.Context, cargo CargoTool, w io.Writer, annot output.Annotator, in CargoPrerequisitesInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	artifacts, err := plannedCargoArtifacts(in.ConfigPlanJSON, in.PublishStagePlanJSON)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		annot.Noticef("No Cargo artefacts")

		return nil
	}

	ok := true
	seen := map[string]bool{}

	for _, artifact := range artifacts {
		dir, dirErr := safeWorkingDir(artifact.WorkingDirectory)
		if dirErr != nil {
			return dirErr
		}

		if seen[dir] {
			continue
		}

		seen[dir] = true
		if !fileExists(filepath.Join(dir, "Cargo.lock")) {
			annot.Errorf("Cargo.lock not found in %s", displayDir(dir))
			annot.Errorf("Cargo.lock must be committed for reproducible releases")

			ok = false
		} else {
			annot.Noticef("Cargo.lock present in %s", displayDir(dir))
		}

		if !fileExists(filepath.Join(dir, "rust-toolchain.toml")) && !fileExists(filepath.Join(dir, "rust-toolchain")) {
			annot.Errorf("No rust-toolchain.toml or rust-toolchain pin found in %s", displayDir(dir))
			annot.Errorf("Pin the toolchain so CI and local dev produce byte-identical binaries")

			ok = false
		} else {
			annot.Noticef("Toolchain pin present in %s", displayDir(dir))
		}
	}

	version, err := cargo.Version(ctx)
	if err != nil {
		annot.Errorf("cargo is not installed on the runner")

		ok = false
	} else if version != "" && w != nil {
		_, _ = fmt.Fprintln(w, version)
	}

	if !ok {
		return fmt.Errorf("cargo prerequisites failed: %w", errs.ErrInvalidConfig)
	}

	return nil
}

// plannedCargoArtifacts pulls every cargo artefact (both build-modes)
// from whichever plan input the caller supplied. ConfigPlanJSON is
// preferred — it carries the canonical artefact list. The publish-stage
// fallback is the historical input; it sees only container-first cargo
// and is kept so direct callers without the config plan still get
// SOMETHING checked (with a notice that artefact-first cargo is invisible
// on this code path).
func plannedCargoArtifacts(configPlanJSON, publishStagePlanJSON string) ([]pipeline.PlannedArtifact, error) {
	if strings.TrimSpace(configPlanJSON) != "" {
		var plan pipeline.ConfigPlan
		if err := json.Unmarshal([]byte(configPlanJSON), &plan); err != nil {
			return nil, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
		}

		if plan.Version != pipeline.ConfigPlanVersion {
			return nil, fmt.Errorf("config-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
		}

		return plan.Artifacts.Cargo, nil
	}

	if strings.TrimSpace(publishStagePlanJSON) == "" {
		return nil, fmt.Errorf("config-plan-json or publish-stage-plan-json is required: %w", errs.ErrUsage)
	}

	var plan pipeline.ReleasePublishStagePlan
	if err := json.Unmarshal([]byte(publishStagePlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("parse publish-stage-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ReleasePlanVersion {
		return nil, fmt.Errorf("publish-stage-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	if plan.Stage != "publish" {
		return nil, fmt.Errorf("publish-stage-plan-json has unexpected stage %q: %w", plan.Stage, errs.ErrInvalidConfig)
	}

	if !plan.Targets.CargoContainerFirst.Runs {
		return nil, nil
	}

	return plan.Targets.CargoContainerFirst.Items, nil
}

// safeWorkingDir / fileExists / displayDir live in workspacedir.go —
// shared with the jvm-reproducibility validator.
