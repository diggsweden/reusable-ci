// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package version wires `reusable-ci version <subcmd>` using urfave/cli v3.
package version

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/commonflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// New returns the `version` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "version-bump and tag-management helpers",
		Commands: []*cli.Command{
			commitPushCmd(),
			renderChangelogCmd(),
			commitChangelogReleaseCmd(),
			deriveReleaseCmd(),
			tagReleaseCmd(),
			generateDevCmd(),
			bumpCmd(),
			filePatternCmd(),
		},
	}
}

func commitPushCmd() *cli.Command {
	return &cli.Command{
		Name:  "commit-push",
		Usage: "stage a file pattern, commit with --signoff, push to a branch (no-op when nothing changed)",
		Description: `EXAMPLE:
   reusable-ci version commit-push --branch main --message "chore: bump to 1.2.3" \
     --file-pattern "pom.xml" --author-name ci-bot --author-email ci@example.com`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagBranch, Required: true, Sources: cli.EnvVars("BRANCH"), Usage: "remote branch to push the commit to"},
			&cli.StringFlag{Name: "author-name", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_NAME"), Usage: "git author name written to the commit"},
			&cli.StringFlag{Name: "author-email", Required: true, Sources: cli.EnvVars("COMMIT_AUTHOR_EMAIL"), Usage: "git author email written to the commit"},
			&cli.StringFlag{Name: "message", Required: true, Sources: cli.EnvVars("COMMIT_MESSAGE"), Usage: "commit message subject (signoff is appended automatically)"},
			&cli.StringFlag{Name: "file-pattern", Required: true, Sources: cli.EnvVars("FILE_PATTERN"), Usage: "whitespace-separated git pathspecs to stage"},
			&cli.StringFlag{Name: flagToken, Sources: cienv.ReleaseToken(), Usage: "token authenticating the push; sent as a transient auth header, never written to .git/config or argv. Required when the checkout did not persist credentials (e.g. `platform checkout`)."}, //nolint:lll // single-line flag declaration for grep-ability, matching the package convention.
			dryrun.Flag("git mutations (author config, commit, push)"),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appversion.CommitPush(ctx, git.New(), os.Stderr, appversion.CommitPushInput{
				Branch:      cmd.String(flagBranch),
				AuthorName:  cmd.String("author-name"),
				AuthorEmail: cmd.String("author-email"),
				Message:     cmd.String("message"),
				FilePattern: cmd.String("file-pattern"),
				Token:       cmd.String(flagToken),
				DryRun:      dryrun.Enabled(cmd),
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
			commonflags.WorkingDir("directory containing the project root"),
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
		Name:  "generate-snapshot",
		Usage: "print a development version tag (`<base>-snapshot-<branch>-<short-sha>`) to stdout",
		Description: `EXAMPLE:
   # Prints e.g. 1.2.3-snapshot-feature-x-abc1234 to stdout
   reusable-ci version generate-snapshot --ref-name feature/x`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "ref-name",
				Usage:   "source branch / ref to sanitise into the snapshot-version suffix",
				Sources: cienv.RefName(),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format, err := deps.OutputFormat(cmd)
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				// generate-snapshot's primary output is the dev version string —
				// designed to be captured via $(...) or piped. Per clig.dev
				// the value goes to stdout; progress/errors go to stderr.
				return appversion.GenerateSnapshotVersion(ctx, git.New(), os.Stdout, appversion.GenerateSnapshotVersionInput{
					RefName: cmd.String("ref-name"),
					Format:  format,
					Sink:    d.OutputSink,
				})
			})
		},
	}
}

func deriveReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "derive-release",
		Usage: "derive the release tag, version and original-tagger commit trailers from the pushed request ref (release-request/vX.Y.Z); emits CI outputs so workflows don't parse refs in bash",
		Description: `EXAMPLE:
   reusable-ci version derive-release --ref release-request/v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "ref",
				Required: true,
				Sources:  cienv.RefName(),
				Usage:    "the pushed ref (e.g. release-request/v1.2.3)",
			},
			&cli.BoolFlag{Name: "require-release-request", Sources: cli.EnvVars("RELEASE_CONTEXT_REQUIRE_REQUEST"), Usage: "reject refs outside release-request/vMAJOR.MINOR.PATCH"},
			&cli.BoolFlag{Name: "require-stable", Sources: cli.EnvVars("RELEASE_CONTEXT_REQUIRE_STABLE"), Usage: "reject final tags outside stable vMAJOR.MINOR.PATCH"},
			&cli.StringFlag{Name: "trailer-mode", Value: "default", Sources: cli.EnvVars("RELEASE_CONTEXT_TRAILER_MODE"), Usage: "commit trailer mode: default (Release-Authorized-By + Co-authored-by) or coauthor-only"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				return appversion.ReleaseContext(ctx, git.New(),
					appversion.ReleaseContextInput{
						Ref:                   cmd.String("ref"),
						RequireReleaseRequest: cmd.Bool("require-release-request"),
						RequireStable:         cmd.Bool("require-stable"),
						TrailerMode:           cmd.String("trailer-mode"),
					},
					d.OutputSink, d.ManifestSink, os.Stderr)
			})
		},
	}
}

func tagReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "tag-release",
		Usage: "create the final release tag once at HEAD (the bump commit) and push it without --force; refuses to move an existing tag",
		Description: `EXAMPLE:
   reusable-ci version tag-release --tag v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagTag,
				Required: true,
				Sources:  cli.EnvVars("RELEASE_TAG", "TAG_NAME"),
				Usage:    "final release tag to create (e.g. v1.2.3)",
			},
			&cli.BoolFlag{
				Name:  "no-sign",
				Usage: "skip GPG signing (intended for tests; production always signs)",
			},
			&cli.BoolFlag{
				Name:    "signed",
				Value:   true,
				Sources: cli.EnvVars("TAG_RELEASE_SIGNED"),
				Usage:   "create a signed tag; set TAG_RELEASE_SIGNED=false for unsigned annotated test tags",
			},
			&cli.StringFlag{Name: flagToken, Sources: cienv.ReleaseToken(), Usage: "token authenticating the tag push; sent as a transient auth header, never written to .git/config or argv. Required when the checkout did not persist credentials (e.g. `platform checkout`)."}, //nolint:lll // single-line flag declaration for grep-ability, matching the package convention.
			dryrun.Flag("git mutations (tag create, tag push)"),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := appversion.TagRelease(ctx, git.New(),
					appversion.TagReleaseInput{Tag: cmd.String(flagTag), Signed: cmd.Bool("signed") && !cmd.Bool("no-sign"), Token: cmd.String(flagToken), DryRun: dryrun.Enabled(cmd)},
					d.OutputSink, os.Stderr)

				return err
			})
		},
	}
}
