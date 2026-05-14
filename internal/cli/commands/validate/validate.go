// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package validate wires `reusable-ci validate ...` subcommands using
// urfave/cli v3.
package validate

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// New returns the `validate` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "input / state validators (ref-type, tag-format, token, …)",
		Commands: []*cli.Command{
			workflowInputDefaultsCmd(),
			refTypeCmd(),
			tagFormatCmd(),
			changelogCmd(),
			tagUniquenessCmd(),
			tagCommitCmd(),
			tagSignatureCmd(),
			gpgPublicKeyCmd(),
			tokenCmd(),
			botPermissionsCmd(),
			authorizationCmd(),
			mavenCentralCredentialsCmd(),
		},
	}
}

func workflowInputDefaultsCmd() *cli.Command {
	return &cli.Command{
		Name:  "workflow-input-defaults",
		Usage: "verify reusable workflow_call input defaults are literal values, not expressions",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "repository root containing .github/workflows"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.WorkflowInputDefaults(os.Stdout, appvalidate.WorkflowInputDefaultsInput{
				Root: cmd.String("root"),
			})
		},
	}
}

func mavenCentralCredentialsCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-central-credentials",
		Usage: "verify $MAVEN_CENTRAL_USERNAME and $MAVEN_CENTRAL_PASSWORD are set",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "username", Sources: cli.EnvVars("MAVEN_CENTRAL_USERNAME")},
			&cli.StringFlag{Name: "password", Sources: cli.EnvVars("MAVEN_CENTRAL_PASSWORD")},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			return appvalidate.MavenCentralCredentials(os.Stdout, os.Stderr, annot, appvalidate.MavenCentralCredentialsInput{
				Username: cmd.String("username"),
				Password: cmd.String("password"),
			})
		},
	}
}

func refTypeCmd() *cli.Command {
	return &cli.Command{
		Name:      "ref-type",
		Usage:     "require that the trigger is a tag push",
		ArgsUsage: "<ref-type> <ref-name> [ref]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 2 {
				return fmt.Errorf("Usage: ref-type <ref-type> <ref-name> [ref]: %w", errs.ErrUsage)
			}
			return appvalidate.RefType(os.Stdout, appvalidate.RefTypeInput{
				RefType: provider.RefType(cmd.Args().Get(0)),
				RefName: cmd.Args().Get(1),
				Ref:     cmd.Args().Get(2),
			})
		},
	}
}

func tagFormatCmd() *cli.Command {
	return &cli.Command{
		Name:      "tag-format",
		Usage:     "validate a tag against the project's permissive semver pattern",
		ArgsUsage: "<tag-name>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: tag-format <tag-name>: %w", errs.ErrUsage)
			}
			return appvalidate.TagFormat(os.Stdout, appvalidate.TagFormatInput{Tag: args[0]})
		},
	}
}

func tagUniquenessCmd() *cli.Command {
	return &cli.Command{
		Name:      "tag-uniqueness",
		Usage:     "fail when other tags point to the same commit (git-cliff guard)",
		ArgsUsage: "<tag-name>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: tag-uniqueness <tag-name>: %w", errs.ErrUsage)
			}
			return appvalidate.TagUniqueness(ctx, git.New(), os.Stdout,
				appvalidate.TagUniquenessInput{Tag: args[0]})
		},
	}
}

func tagCommitCmd() *cli.Command {
	return &cli.Command{
		Name:      "tag-commit",
		Usage:     "verify the tag commit is reachable from origin/<branch>",
		ArgsUsage: "<tag-name> [branch]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 1 {
				return fmt.Errorf("Usage: tag-commit <tag-name> [branch]: %w", errs.ErrUsage)
			}
			return appvalidate.TagCommit(ctx, git.New(), os.Stdout, appvalidate.TagCommitInput{
				Tag:    cmd.Args().Get(0),
				Branch: cmd.Args().Get(1),
			})
		},
	}
}

func tagSignatureCmd() *cli.Command {
	return &cli.Command{
		Name:      "tag-signature",
		Usage:     "verify a tag is annotated and cryptographically signed (GPG or SSH)",
		ArgsUsage: "<tag-name> [repository]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "release-gpg-public-key",
				Sources: cli.EnvVars("RELEASE_GPG_PUBLIC_KEY"),
				Usage:   "armored GPG public key to import before verification",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 1 {
				return fmt.Errorf("Usage: tag-signature <tag-name> [repository]: %w", errs.ErrUsage)
			}
			in := appvalidate.TagSignatureInput{
				Tag:        cmd.Args().Get(0),
				Repository: cmd.Args().Get(1),
			}
			if k := cmd.String("release-gpg-public-key"); k != "" {
				in.ReleaseGPGPublicKey = []byte(k)
			}
			return appvalidate.TagSignature(ctx, git.New(), gpg.New(), os.Stdout, in)
		},
	}
}

func tokenCmd() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "validate a release-bot token against the platform API",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "token",
				Required: true,
				Usage:    "release-bot token to validate (prefer the env source to keep it out of shell history / ps)",
				Sources:  cli.EnvVars("RELEASE_TOKEN"),
			},
			&cli.StringFlag{
				Name:     "repository",
				Required: true,
				Usage:    "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab",
				Sources:  cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appvalidate.Token(ctx, d.Provider, os.Stdout, appvalidate.TokenInput{
				Token:      cmd.String("token"),
				Repository: cmd.String("repository"),
			})
		},
	}
}

func botPermissionsCmd() *cli.Command {
	return &cli.Command{
		Name:      "bot-permissions",
		Usage:     "probe the configured release-bot token's repo + branch access",
		ArgsUsage: "<repository>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: bot-permissions <repository>: %w", errs.ErrUsage)
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appvalidate.BotPermissions(ctx, d.Provider, os.Stdout, appvalidate.BotPermissionsInput{
				Repository: args[0],
			})
		},
	}
}

func authorizationCmd() *cli.Command {
	return &cli.Command{
		Name:      "authorization",
		Usage:     "gate non-SNAPSHOT releases on a configured user CSV",
		ArgsUsage: "<tag-name> <actor> [authorized-devs]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "authorized-devs",
				Sources: cli.EnvVars("RELEASE_AUTHORIZED_USERS"),
				Usage:   "comma-separated list of usernames allowed to release",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 2 {
				return fmt.Errorf("Usage: authorization <tag-name> <actor> [authorized-devs]: %w", errs.ErrUsage)
			}
			devs := cmd.String("authorized-devs")
			if devs == "" {
				devs = cmd.Args().Get(2)
			}
			return appvalidate.Authorization(os.Stdout, appvalidate.AuthorizationInput{
				Tag: cmd.Args().Get(0), Actor: cmd.Args().Get(1), AuthorizedDevs: devs,
			})
		},
	}
}

func gpgPublicKeyCmd() *cli.Command {
	return &cli.Command{
		Name:  "gpg-public-key",
		Usage: "fail when RELEASE_GPG_PUBLIC_KEY is unset",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "release-gpg-public-key",
				Sources: cli.EnvVars("RELEASE_GPG_PUBLIC_KEY"),
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.GPGPublicKey(os.Stdout, cmd.String("release-gpg-public-key"))
		},
	}
}

func changelogCmd() *cli.Command {
	return &cli.Command{
		Name:  "changelog",
		Usage: "verify a changelog file's presence (or read its content into the output sink)",
		Description: "Two modes — full (the file must exist; emits a line count) and " +
			"minimal (file may be absent; emits content=<file body> or content=" +
			"\"No changes for this release\" via the OutputSink).",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "path",
				Required: true,
				Usage:    "changelog file path",
			},
			&cli.BoolFlag{
				Name:  "required",
				Usage: "fail when the file is missing (full-changelog mode)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appvalidate.Changelog(ctx, d.OutputSink, os.Stdout, appvalidate.ChangelogInput{
				Path:     cmd.String("path"),
				Required: cmd.Bool("required"),
			})
		},
	}
}
