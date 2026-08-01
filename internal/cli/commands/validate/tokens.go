// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
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
		Description: `EXAMPLE:
   # Token from $RELEASE_TOKEN (or --token-file -)
   reusable-ci validate auth token --repository org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "token-file",
				Usage: "path to a file containing the release-bot token (use \"-\" for stdin; defaults to $RELEASE_TOKEN)",
			},
			&cli.StringFlag{
				Name:     "repository", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Required: true,
				Usage:    "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources:  cienv.Repository(),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			token, err := secret.Resolve(cmd.String("token-file"), "RELEASE_TOKEN")
			if err != nil {
				return err
			}

			if token == "" {
				return errs.CredentialRequired(errs.Credential{What: "release-bot token", Flag: "token-file", Env: "RELEASE_TOKEN"})
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				tv, err := d.RequireTokenValidator()
				if err != nil {
					return err
				}

				return appvalidate.Token(ctx, tv, os.Stderr, appvalidate.TokenInput{
					Token:      token,
					Repository: cmd.String("repository"),
				})
			})
		},
	}
}

func authBotPermissionsCmd() *cli.Command {
	return &cli.Command{
		Name:  "bot-permissions",
		Usage: "probe the configured release-bot token's repo + branch access",
		Description: `EXAMPLE:
   reusable-ci validate auth bot-permissions --repository org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "repository",
				Required: true,
				Sources:  cienv.Repository(),
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
		Description: `EXAMPLE:
   reusable-ci validate auth registry --registry ghcr.io --use-ci-token`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "use-ci-token", Sources: cli.EnvVars("USE_CI_TOKEN"), Usage: "the CI platform token is used in place of an explicit registry password"},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("REGISTRY"), Usage: "registry hostname the workflow targets"},
			&cli.StringFlag{Name: "expected-registry", Value: container.DefaultRegistry, Sources: cli.EnvVars("CI_REGISTRY"), Usage: "registry hostname the CI token is valid for"},
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
