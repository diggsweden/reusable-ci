// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gradle"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func gradleCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle",
		Usage: "gradle (JVM) build wrappers",
		Commands: []*cli.Command{
			gradleMetadataCmd(),
			gradleApplicationCmd(),
			gradleSBOMCmd(),
		},
	}
}

func gradleApplicationCmd() *cli.Command {
	return &cli.Command{
		Name:  "application", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "run `./gradlew <tasks>` with optional `-x test`",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tasks", Required: true, Sources: cli.EnvVars("GRADLE_TASKS"), Usage: "whitespace-separated gradle tasks to run"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "append -x test to skip the test task"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.GradleApplication(ctx, gradle.New(), os.Stderr, os.Stderr, appbuild.GradleApplicationInput{
				Tasks:     cmd.String("tasks"),
				SkipTests: cmd.Bool("skip-tests"),
			})
		},
	}
}

func gradleMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read gradle.properties metadata and emit CI outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing gradle.properties / build.gradle"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.GradleMetadata(ctx, d.OutputSink, os.Stderr, annot, appbuild.GradleMetadataInput{Dir: cmd.String("working-dir")})
			})
		},
	}
}

func gradleSBOMCmd() *cli.Command {
	return &cli.Command{
		Name:  "sbom",
		Usage: "generate a CycloneDX Build SBOM via the cyclonedx-gradle-plugin",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "cyclonedx-version",
				Usage:   "pinned cyclonedx-gradle-plugin version",
				Sources: cli.EnvVars("CYCLONEDX_GRADLE_VERSION"),
			},
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing gradle.properties / build.gradle"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			version := cmd.String("cyclonedx-version")
			if version == "" {
				return fmt.Errorf("--cyclonedx-version (or $CYCLONEDX_GRADLE_VERSION) is required: %w", errs.ErrUsage)
			}

			return appbuild.GradleSBOM(ctx, gradle.New(), os.Stderr, os.Stderr, appbuild.GradleSBOMInput{
				CycloneDXVersion: version,
				WorkingDir:       cmd.String("working-dir"),
			})
		},
	}
}
