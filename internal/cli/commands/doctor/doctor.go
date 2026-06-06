// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package doctor wires `reusable-ci doctor` — runs a fixed set of
// setup-validation checks against the current repository and emits
// an OK/WARN/FAIL report. Designed for "I just adopted reusable-ci,
// am I set up correctly?" UX.
package doctor

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appdoctor "github.com/diggsweden/reusable-ci/internal/app/doctor"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// New returns the `doctor` top-level subcommand.
func New() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "run setup-validation checks against the current repository (artifacts.yml present + valid, release-authorization allowlist when required, workflow id-token permission for sigstore, workflows pin reusable-ci to a tag)",
		Description: `EXAMPLES:
   # Run all checks in the current directory:
   reusable-ci doctor

   # Run against a specific repo root:
   reusable-ci doctor --root /path/to/repo

   # Use a custom artifacts.yml location:
   reusable-ci doctor --artifacts ./custom/artifacts.yml

Exit codes:
   0   — all checks pass or are warnings
   1   — at least one FAIL check (ExitCodeValidation)`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "root",
				Sources: cli.EnvVars("REUSABLE_CI_DOCTOR_ROOT"),
				Usage:   "repository root to inspect (default: cwd)",
			},
			&cli.StringFlag{
				Name:    "artifacts",
				Sources: cli.EnvVars("REUSABLE_CI_DOCTOR_ARTIFACTS"),
				Usage:   "override the artifacts.yml lookup (default: <root>/.reusable-ci/artifacts.yml)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			checks, err := appdoctor.Run(appdoctor.Input{
				Root:          cmd.String("root"),
				ArtifactsPath: cmd.String("artifacts"),
			})
			if err != nil {
				return err
			}

			appdoctor.FormatText(os.Stdout, checks)

			failures := 0

			for _, c := range checks {
				if c.Severity == appdoctor.SeverityFail {
					failures++
				}
			}

			if failures > 0 {
				// Wrap ErrValidation so main maps the exit code via
				// errs.ExitCodeFromError; the message complements
				// (not duplicates) the FormatText output above.
				return fmt.Errorf("doctor: %d failing check(s); see output above: %w", failures, errs.ErrValidation)
			}

			return nil
		},
	}
}
