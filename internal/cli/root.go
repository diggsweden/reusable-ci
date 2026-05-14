// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package cli assembles the top-level urfave/cli v3 command tree and
// related CLI-facing helpers.
//
// The package wires global flags, subgroups, and docs rendering. It does not
// implement any business logic — that lives under internal/app/.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	cmdbuild "github.com/diggsweden/reusable-ci/internal/cli/commands/build"
	cmdci "github.com/diggsweden/reusable-ci/internal/cli/commands/ci"
	cmdconfig "github.com/diggsweden/reusable-ci/internal/cli/commands/config"
	cmdcontainer "github.com/diggsweden/reusable-ci/internal/cli/commands/container"
	cmdplan "github.com/diggsweden/reusable-ci/internal/cli/commands/plan"
	cmdpublish "github.com/diggsweden/reusable-ci/internal/cli/commands/publish"
	cmdrelease "github.com/diggsweden/reusable-ci/internal/cli/commands/release"
	cmdsbom "github.com/diggsweden/reusable-ci/internal/cli/commands/sbom"
	cmdsecurity "github.com/diggsweden/reusable-ci/internal/cli/commands/security"
	cmdsummary "github.com/diggsweden/reusable-ci/internal/cli/commands/summary"
	cmdvalidate "github.com/diggsweden/reusable-ci/internal/cli/commands/validate"
	cmdversion "github.com/diggsweden/reusable-ci/internal/cli/commands/version"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainlog "github.com/diggsweden/reusable-ci/internal/domain/log"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// BuildInfo carries ldflags-injected version metadata from main.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// New builds the root *cli.Command. Subgroups are added here as each
// migration phase lands them.
func New(b BuildInfo) *cli.Command {
	versionString := fmt.Sprintf("%s (commit %s, built %s)", b.Version, b.Commit, b.Date)

	return &cli.Command{
		Name:    "reusable-ci",
		Usage:   "shared CI/CD logic for diggsweden/reusable-ci workflows",
		Version: versionString,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "suppress info-level log output",
				Sources: cli.EnvVars("REUSABLE_CI_QUIET"),
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "logging level: trace, debug, info, warn, error",
				Value:   "info",
				Sources: cli.EnvVars("REUSABLE_CI_LOG"),
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "output format: auto, text, json, github, gitlab",
				Value:   string(output.FormatAuto),
				Sources: cli.EnvVars("REUSABLE_CI_OUTPUT"),
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if err := configureLogger(cmd); err != nil {
				return ctx, err
			}
			if _, err := output.Parse(cmd.String("output")); err != nil {
				return ctx, fmt.Errorf("%w: %w", err, errs.ErrUsage)
			}
			return ctx, nil
		},
		Commands: []*cli.Command{
			cmdbuild.New(),
			cmdci.New(),
			cmdconfig.New(),
			cmdcontainer.New(),
			cmdplan.New(),
			cmdpublish.New(),
			cmdrelease.New(),
			cmdsbom.New(),
			cmdsecurity.New(),
			cmdsummary.New(),
			cmdvalidate.New(),
			cmdversion.New(),
		},
	}
}

// configureLogger wires slog to stderr at the requested level.
// Quiet mode raises the threshold to error.
func configureLogger(cmd *cli.Command) error {
	level := cmd.String("log-level")
	if cmd.Bool("quiet") {
		level = "error"
	}

	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "trace":
		slogLevel = domainlog.LevelTrace
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		return fmt.Errorf("invalid log-level %q (want trace/debug/info/warn/error): %w", level, errs.ErrUsage)
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slogLevel,
		ReplaceAttr: domainlog.ReplaceLevelAttr,
	})
	slog.SetDefault(slog.New(handler))
	return nil
}
