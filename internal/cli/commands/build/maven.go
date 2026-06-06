// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func mavenCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven",
		Usage: "maven build wrappers",
		Commands: []*cli.Command{
			mavenMetadataCmd(),
			mavenApplicationCmd(),
			mavenLibraryCmd(),
		},
	}
}

func mavenApplicationCmd() *cli.Command {
	return &cli.Command{
		Name:  "application", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "run `mvn clean package` with optional -DskipTests",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cli-opts", Sources: cli.EnvVars("MAVEN_CLI_OPTS"), Usage: "extra args forwarded to mvn (whitespace-separated, e.g. \"-B -ntp\")"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "pass -DskipTests=true to the Maven package phase"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.MavenApplication(ctx, maven.New(), os.Stderr, os.Stderr, appbuild.MavenApplicationInput{
				CLIOpts:   strings.Fields(cmd.String("cli-opts")),
				SkipTests: cmd.Bool("skip-tests"),
			})
		},
	}
}

func mavenMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "parse pom.xml for {version,groupId,artifactId} and emit CI outputs (falls back to `mvn help:evaluate` only for ${property} references)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the pom.xml to parse"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.MavenMetadata(ctx, d.OutputSink, maven.New(), os.Stderr, appbuild.MavenMetadataInput{
					Dir: cmd.String("working-dir"),
				})
			})
		},
	}
}

func mavenLibraryCmd() *cli.Command {
	return &cli.Command{
		Name:  "library",
		Usage: "build a Maven library with sources and javadoc JARs",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "cli-opts",
				Usage:   "space-separated mvn CLI options (e.g. --batch-mode --errors)",
				Sources: cli.EnvVars("MAVEN_CLI_OPTS"),
			},
			&cli.StringFlag{
				Name:    "profile",
				Usage:   "Maven profile to activate",
				Sources: cli.EnvVars("MAVEN_PROFILE"),
			},
			&cli.BoolFlag{
				Name:    "skip-tests",
				Usage:   "skip the test phase (and propagate -DskipTests=true to package)",
				Sources: cli.EnvVars("SKIP_TESTS"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.MavenLibrary(ctx, maven.New(), os.Stderr, os.Stderr, appbuild.MavenLibraryInput{
				CLIOpts:   strings.Fields(cmd.String("cli-opts")),
				Profile:   cmd.String("profile"),
				SkipTests: cmd.Bool("skip-tests"),
			})
		},
	}
}
