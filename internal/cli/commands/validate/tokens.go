// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// authGroup wires `reusable-ci validate auth <verb>` — release-flow
// authentication checks: probe a release-bot token against the
// platform API, probe the bot's repo/branch access, gate non-SNAPSHOT
// releases via the bot-permissions API, and presence-check the
// container/package registry password before a publish step runs.
func authGroup() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "validate one aspect of release-flow authentication",
		Commands: []*cli.Command{
			authTokenCmd(),
			authBotPermissionsCmd(),
			authRegistryCmd(),
		},
	}
}

func authTokenCmd() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "validate a release-bot token against the platform API",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "token-file",
				Usage: "path to a file containing the release-bot token (use \"-\" for stdin; defaults to $RELEASE_TOKEN)",
			},
			&cli.StringFlag{
				Name:     "repository", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Required: true,
				Usage:    "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources:  cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			token, err := secret.Resolve(cmd.String("token-file"), "RELEASE_TOKEN")
			if err != nil {
				return err
			}

			if token == "" {
				return fmt.Errorf("a release-bot token is required: pass --token-file <path> (\"-\" for stdin) or set $RELEASE_TOKEN: %w", errs.ErrUsage)
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				tv, err := d.RequireTokenValidator()
				if err != nil {
					return err
				}

				return appvalidate.Token(ctx, tv, os.Stderr, appvalidate.TokenInput{
					Token:      token,
					Repository: cmd.String("repository"),
					Platform:   d.Platform,
				})
			})
		},
	}
}

func authBotPermissionsCmd() *cli.Command {
	return &cli.Command{
		Name:  "bot-permissions",
		Usage: "probe the configured release-bot token's repo + branch access",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "repository",
				Required: true,
				Sources:  cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"),
				Usage:    "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				tv, err := d.RequireTokenValidator()
				if err != nil {
					return err
				}

				return appvalidate.BotPermissions(ctx, tv, os.Stderr, appvalidate.BotPermissionsInput{
					Repository: cmd.String("repository"),
				})
			})
		},
	}
}

func authRegistryCmd() *cli.Command {
	return &cli.Command{
		Name:  "registry",
		Usage: "validate container/package registry authentication configuration before publishing",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "use-ci-token", Sources: cli.EnvVars("USE_CI_TOKEN"), Usage: "the CI platform token is used in place of an explicit registry password"},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("REGISTRY"), Usage: "registry hostname the workflow targets"},
			&cli.StringFlag{Name: "expected-registry", Value: "ghcr.io", Sources: cli.EnvVars("CI_REGISTRY"), Usage: "registry hostname the CI token is valid for"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			// Presence-check on $REGISTRY_PASSWORD via env (the value
			// never reaches argv).
			hasPassword := os.Getenv("REGISTRY_PASSWORD") != ""

			return apppublish.RegistryAuth(ctx, os.Stderr, os.Stderr, annot, publish.RegistryAuthInput{
				UseCIToken:       cmd.Bool("use-ci-token"),
				Registry:         cmd.String("registry"),
				ExpectedRegistry: cmd.String("expected-registry"),
				HasPassword:      hasPassword,
			})
		},
	}
}
