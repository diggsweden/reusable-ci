// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package config wires `reusable-ci config <subcmd>` using urfave/cli v3.
package config

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appconfig "github.com/diggsweden/reusable-ci/internal/app/config"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
		Name:      "parse-artifacts",
		Usage:     "parse artifacts.yml and emit per-type / per-publish-target / SBOM outputs",
		ArgsUsage: "[path]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Sources: cli.EnvVars("ARTIFACTS_CONFIG"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path := cmd.String("file")
			if path == "" && cmd.Args().Len() > 0 {
				path = cmd.Args().First()
			}
			if path == "" {
				return fmt.Errorf("config parse-artifacts: missing path (pass as arg or --file/$ARTIFACTS_CONFIG): %w", errs.ErrUsage)
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appconfig.ParseArtifacts(ctx, d.OutputSink, d.SummarySink, os.Stderr, annot,
				appconfig.ParseArtifactsInput{Path: path})
		},
	}
}

func expandSBOMsCmd() *cli.Command {
	return &cli.Command{
		Name:      "expand-sboms",
		Usage:     "expand an `sboms` enum value to a layer list (json or comma)",
		ArgsUsage: "<value>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Value: "json", Usage: "json | comma"},
			&cli.StringSliceFlag{Name: "exclude", Usage: "drop one or more layers from the expansion"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: expand-sboms [--format json|comma] [--exclude <layer>] <value>: %w", errs.ErrUsage)
			}
			format := appconfig.ExpandSBOMsFormat(cmd.String("format"))
			return appconfig.ExpandSBOMs(os.Stdout, appconfig.ExpandSBOMsInput{
				Value:   args[0],
				Format:  format,
				Exclude: cmd.StringSlice("exclude"),
			})
		},
	}
}

func validateCmd() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Usage:     "validate an artifacts.yml against the schema",
		ArgsUsage: "<path>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "path to artifacts.yml (overrides positional)",
				Sources: cli.EnvVars("ARTIFACTS_CONFIG"),
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			path := cmd.String("file")
			if path == "" && cmd.Args().Len() > 0 {
				path = cmd.Args().First()
			}
			if path == "" {
				return fmt.Errorf("config validate: missing path (pass as arg or --file/$ARTIFACTS_CONFIG): %w", errs.ErrUsage)
			}
			if err := appconfig.Validate(path, os.Stderr); err != nil {
				return err
			}
			fmt.Printf("✓ %s is valid\n", path)
			return nil
		},
	}
}
