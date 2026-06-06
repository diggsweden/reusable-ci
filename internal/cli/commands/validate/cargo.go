// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cargo"
	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func cargoCmd() *cli.Command {
	return &cli.Command{
		Name:  "cargo",
		Usage: "verify Cargo.lock/toolchain state for every planned Cargo artefact (both build-modes)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON; carries every Cargo artefact regardless of build-mode (preferred input)"},
			&cli.StringFlag{Name: "publish-stage-plan-json", Sources: cli.EnvVars("PUBLISH_STAGE_PLAN_JSON"), Usage: "typed publish-stage plan JSON listing container-first Cargo artefacts (fallback when config-plan-json is not available; misses artefact-first cargo)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appvalidate.CargoPrerequisites(ctx, cargo.New(), os.Stderr, annot, appvalidate.CargoPrerequisitesInput{
				ConfigPlanJSON:       cmd.String("config-plan-json"),
				PublishStagePlanJSON: cmd.String("publish-stage-plan-json"),
			})
		},
	}
}
