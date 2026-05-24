// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package version wires `reusable-ci version <subcmd>` using urfave/cli v3.
package version

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/internal/adapters/npm"
	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
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
			&cli.StringFlag{Name: "branch", Required: true, Sources: cli.EnvVars("BRANCH"), Usage: "remote branch to push the commit to"},
			&cli.StringFlag{Name: "author-name", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_NAME"), Usage: "git author name written to the commit"},
			&cli.StringFlag{Name: "author-email", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_EMAIL"), Usage: "git author email written to the commit"},
			&cli.StringFlag{Name: "message", Required: true, Sources: cli.EnvVars("COMMIT_MESSAGE"), Usage: "commit message subject (signoff is appended automatically)"},
			&cli.StringFlag{Name: "file-pattern", Required: true, Sources: cli.EnvVars("FILE_PATTERN"), Usage: "whitespace-separated git pathspecs to stage"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appversion.CommitPush(ctx, git.New(), os.Stderr, appversion.CommitPushInput{
				Branch:      cmd.String("branch"),
				AuthorName:  cmd.String("author-name"),
				AuthorEmail: cmd.String("author-email"),
				Message:     cmd.String("message"),
				FilePattern: cmd.String("file-pattern"),
			})
		},
	}
}

func bumpCmd() *cli.Command {
	return &cli.Command{
		Name:  "bump",
		Usage: "rewrite the version-of-record (Maven POM / package.json / gradle.properties / .xcconfig / Cargo.toml)",
		Description: `EXAMPLES:
   # Bump a Maven project to 1.2.3 (updates pom.xml and every child)
   reusable-ci version bump --project-type=maven --version=1.2.3

   # Bump an NPM package; --working-dir locates the project root
   reusable-ci version bump --project-type=npm --version=2.0.0 --working-dir=./app

   # Bump a Cargo workspace (the [workspace.package].version field)
   reusable-ci version bump --project-type=cargo --version=0.5.0`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project-type",
				Required: true,
				Sources:  cli.EnvVars("PROJECT_TYPE"),
				Usage:    "ecosystem driving the bump (maven/gradle/npm/cargo/xcode-ios)",
			},
			&cli.StringFlag{
				Name:     "version",
				Required: true,
				Sources:  cli.EnvVars("VERSION"),
				Usage:    "new version-of-record (without a leading 'v')",
			},
			&cli.StringFlag{
				Name:    "working-dir",
				Value:   ".",
				Sources: cli.EnvVars("WORKING_DIRECTORY"),
				Usage:   "directory containing the project root",
			},
			&cli.StringFlag{
				Name:    "gradle-version-file",
				Sources: cli.EnvVars("GRADLE_VERSION_FILE"),
				Usage:   "path to the gradle.properties file holding the version key (gradle only)",
			},
			&cli.StringFlag{
				Name:    "xcode-version-file",
				Sources: cli.EnvVars("XCODE_VERSION_FILE"),
				Usage:   "path to the xcconfig file holding MARKETING_VERSION (xcode-ios only)",
			},
			&cli.StringFlag{
				Name:    "maven-cli-opts",
				Sources: cli.EnvVars("MAVEN_CLI_OPTS"),
				Usage:   "extra args forwarded to mvn (whitespace-separated, e.g. \"-B -ntp\")",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			in := appversion.BumpInput{
				ProjectType:       projecttype.Type(cmd.String("project-type")),
				Version:           cmd.String("version"),
				WorkingDir:        cmd.String("working-dir"),
				GradleVersionFile: cmd.String("gradle-version-file"),
				XcconfigFile:      cmd.String("xcode-version-file"),
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

			return appversion.Bump(ctx, ops, os.Stderr, os.Stderr, annot, in)
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
			format, err := deps.OutputFormat(cmd)
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				// generate-dev's primary output is the dev version string —
				// designed to be captured via $(...) or piped. Per clig.dev
				// the value goes to stdout; progress/errors go to stderr.
				return appversion.GenerateDevVersion(ctx, git.New(), os.Stdout, appversion.GenerateDevVersionInput{
					RefName: cmd.String("ref-name"),
					Format:  format,
					Sink:    d.OutputSink,
				})
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
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appversion.MoveTag(ctx, git.New(),
					appversion.MoveTagInput{Signed: !cmd.Bool("no-sign")},
					d.OutputSink, os.Stderr)

				return err
			})
		},
	}
}
