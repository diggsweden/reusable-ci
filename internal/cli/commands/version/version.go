// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package version wires `reusable-ci version <subcmd>` using urfave/cli v3.
package version

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/internal/adapters/npm"
	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/platform"
)

// New returns the `version` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "version-bump and tag-management helpers",
		Commands: []*cli.Command{
			commitPushCmd(),
			moveTagCmd(),
			generateDevCmd(),
			bumpCmd(),
		},
	}
}

func commitPushCmd() *cli.Command {
	return &cli.Command{
		Name:  "commit-push",
		Usage: "stage a file pattern, commit with --signoff, push to a branch (no-op when nothing changed)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "branch", Required: true, Sources: cli.EnvVars("BRANCH")},
			&cli.StringFlag{Name: "author-name", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_NAME")},
			&cli.StringFlag{Name: "author-email", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_EMAIL")},
			&cli.StringFlag{Name: "message", Required: true, Sources: cli.EnvVars("COMMIT_MESSAGE")},
			&cli.StringFlag{Name: "file-pattern", Required: true, Sources: cli.EnvVars("FILE_PATTERN")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appversion.CommitPush(ctx, git.New(), appversion.CommitPushInput{
				Branch:      cmd.String("branch"),
				AuthorName:  cmd.String("author-name"),
				AuthorEmail: cmd.String("author-email"),
				Message:     cmd.String("message"),
				FilePattern: cmd.String("file-pattern"),
			}, os.Stdout)
		},
	}
}

func bumpCmd() *cli.Command {
	return &cli.Command{
		Name:      "bump",
		Usage:     "rewrite the version-of-record (Maven POM / package.json / gradle.properties / .xcconfig / Cargo.toml)",
		ArgsUsage: "<project-type> <version> [working-dir] [version-file]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "xcode-version-file",
				Usage:   "path to the xcconfig file holding MARKETING_VERSION (xcode-ios only)",
				Sources: cli.EnvVars("XCODE_VERSION_FILE"),
			},
			&cli.StringFlag{
				Name:    "maven-cli-opts",
				Usage:   "extra args forwarded to mvn (whitespace-separated, e.g. \"-B -ntp\")",
				Sources: cli.EnvVars("MAVEN_CLI_OPTS"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 2 {
				return fmt.Errorf("Usage: bump <project-type> <version> [working-dir] [version-file]: %w", errs.ErrUsage)
			}
			in := appversion.BumpInput{
				ProjectType: projecttype.Type(args[0]),
				Version:     args[1],
			}
			if len(args) >= 3 {
				in.WorkingDir = args[2]
			}
			if len(args) >= 4 {
				// Bash treats arg-4 as gradle-properties path; xcode-ios uses
				// --xcode-version-file (or XCODE_VERSION_FILE env). The Go port
				// honours both shapes.
				in.GradleVersionFile = args[3]
			}
			if v := cmd.String("xcode-version-file"); v != "" {
				in.XcconfigFile = v
			}
			if opts := cmd.String("maven-cli-opts"); opts != "" {
				in.MavenCLIOpts = strings.Fields(opts)
			}
			ops := appversion.BumpOps{
				Maven: maven.New(),
				NPM:   npm.New(),
				Cargo: cargo.New(),
			}
			annot := deps.Annotator(cmd)
			return appversion.Bump(ctx, ops, os.Stdout, os.Stderr, annot, in)
		},
	}
}

func generateDevCmd() *cli.Command {
	return &cli.Command{
		Name:  "generate-dev",
		Usage: "print a development version tag (`<base>-dev-<branch>-<short-sha>`) to stdout",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "ref-name",
				Usage:   "source branch / ref to sanitise into the dev-version suffix",
				Sources: cli.EnvVars("CI_REF_NAME", "GITHUB_REF_NAME"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format, err := output.ParseAndResolve(cmd.Root().String("output"), platform.Detect())
			if err != nil {
				return err
			}
			return appversion.GenerateDevVersion(ctx, git.New(), os.Stdout, appversion.GenerateDevVersionInput{
				RefName: cmd.String("ref-name"),
				Format:  format,
			})
		},
	}
}

func moveTagCmd() *cli.Command {
	return &cli.Command{
		Name:  "move-tag",
		Usage: "verify the latest tag is at HEAD~1, re-create it signed at HEAD, and push --force",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "no-sign",
				Usage: "skip GPG signing (intended for tests; production always signs)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appversion.MoveTag(ctx, git.New(),
				appversion.MoveTagInput{Signed: !cmd.Bool("no-sign")},
				d.OutputSink, os.Stdout)
			return err
		},
	}
}
