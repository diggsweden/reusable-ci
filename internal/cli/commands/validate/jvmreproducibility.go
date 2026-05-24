// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// jvmReproducibilityCmd exposes the JVM-side counterpart of `validate
// cargo`: scans every planned Maven/Gradle artefact and warns when its
// manifest lacks the reproducibility knob. Warnings, not errors —
// see appvalidate.JVMReproducibility for the rationale.
func jvmReproducibilityCmd() *cli.Command {
	return &cli.Command{
		Name:  "jvm-reproducibility",
		Usage: "warn when Maven/Gradle artefacts lack reproducible-build settings (outputTimestamp / archive-task config)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config-plan-json",
				Sources: cli.EnvVars("CONFIG_PLAN_JSON"),
				Usage:   "typed config-plan JSON (output of 'config parse-artifacts')",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appvalidate.JVMReproducibility(ctx, os.Stderr, annot, appvalidate.JVMReproducibilityInput{
				ConfigPlanJSON: cmd.String("config-plan-json"),
			})
		},
	}
}
