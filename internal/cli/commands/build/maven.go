// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
			mavenLibraryCmd(),
		},
	}
}

func mavenMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "extract project.{version,groupId,artifactId} from the POM and emit CI outputs",
		Action: func(ctx context.Context, _ *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appbuild.MavenMetadata(ctx, d.OutputSink, maven.New(), os.Stdout)
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
			return appbuild.MavenLibrary(ctx, maven.New(), os.Stdout, os.Stderr, appbuild.MavenLibraryInput{
				CLIOpts:   strings.Fields(cmd.String("cli-opts")),
				Profile:   cmd.String("profile"),
				SkipTests: cmd.Bool("skip-tests"),
			})
		},
	}
}
