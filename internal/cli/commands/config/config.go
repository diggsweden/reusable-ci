// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package config wires `reusable-ci config <subcmd>` using urfave/cli v3.
package config

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// New returns the `config` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "artifacts.yml schema helpers",
		Commands: []*cli.Command{
			validateCmd(),
			parseArtifactsCmd(),
			expandSBOMsCmd(),
		},
	}
}

func parseArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "parse-artifacts",
		Usage: "parse artifacts.yml and emit the typed config plan contract",
		Description: `EXAMPLES:
   # Parse artifacts.yml; the typed config plan lands on the
   # configured OutputSink ($GITHUB_OUTPUT on GitHub, dotenv on
   # GitLab, /dev/null in local mode).
   reusable-ci config parse-artifacts --file artifacts.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Required: true,
				Sources:  cli.EnvVars("ARTIFACTS_CONFIG"),
				Usage:    "path to the artifacts.yml file to parse",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appconfig.EmitConfigPlan(ctx, d.OutputSink, d.SummarySink, os.Stderr, annot,
					appconfig.EmitConfigPlanInput{Path: cmd.String("file")})
			})
		},
	}
}

func expandSBOMsCmd() *cli.Command {
	return &cli.Command{
		Name:  "expand-sboms",
		Usage: "expand an `sboms` enum value to a layer list (json or comma)",
		Description: `EXAMPLE:
   reusable-ci config expand-sboms --value all --as comma --exclude analyzed-container`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "value",
				Required: true,
				Sources:  cli.EnvVars("SBOMS"),
				Usage:    "sboms enum value to expand (e.g. \"all\", \"build,source\")",
			},
			// `--as` (not `--format`) on purpose: the layer-list shape is a
			// different axis from the global `--format` output renderer, and
			// reusing that name would shadow it with an incompatible value set.
			&cli.StringFlag{Name: "as", Value: "json", Usage: "layer-list shape: json | comma"},
			&cli.StringSliceFlag{Name: "exclude", Usage: "drop one or more layers from the expansion"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			outFormat, err := deps.OutputFormat(cmd)
			if err != nil {
				return err
			}

			format := appconfig.ExpandSBOMsFormat(cmd.String("as"))

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				// expand-sboms's primary output is the expanded layer
				// list (JSON or comma-separated) — designed to be piped
				// or captured. Per clig.dev the value goes to stdout;
				// progress/errors go to stderr.
				return appconfig.ExpandSBOMs(ctx, os.Stdout, appconfig.ExpandSBOMsInput{
					Value:   cmd.String("value"),
					Format:  format,
					Exclude: cmd.StringSlice("exclude"),
					Output:  outFormat,
					Sink:    d.OutputSink,
				})
			})
		},
	}
}

func validateCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "validate an artifacts.yml against the schema",
		Description: `EXAMPLES:
   # Validate the conventional .reusable-ci/artifacts.yml (the default)
   reusable-ci config validate

   # Validate a specific file / via env var (workflow shape)
   reusable-ci config validate --file path/to/artifacts.yml
   ARTIFACTS_CONFIG=artifacts.yml reusable-ci config validate`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Value:   ".reusable-ci/artifacts.yml",
				Sources: cli.EnvVars("ARTIFACTS_CONFIG"),
				Usage:   "path to the artifacts.yml file to validate (defaults to the conventional .reusable-ci/artifacts.yml)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			path := cmd.String("file")
			if err := appconfig.Validate(path, os.Stderr); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "✓ %s is valid\n", path)

			return nil
		},
	}
}
