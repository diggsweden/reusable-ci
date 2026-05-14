// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gradle"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func gradleCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle",
		Usage: "gradle (JVM) build wrappers",
		Commands: []*cli.Command{
			gradleSBOMCmd(),
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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			version := cmd.String("cyclonedx-version")
			if version == "" {
				return fmt.Errorf("--cyclonedx-version (or $CYCLONEDX_GRADLE_VERSION) is required: %w", errs.ErrUsage)
			}
			return appbuild.GradleSBOM(ctx, gradle.New(), os.Stdout, os.Stderr, appbuild.GradleSBOMInput{
				CycloneDXVersion: version,
			})
		},
	}
}
