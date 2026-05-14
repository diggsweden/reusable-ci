// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func devPublishStageResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "dev-publish-stage-result",
		Usage: "compose the dev-publish stage manifest + dual-write outputs (container/npm/cargo-sbom/sbom)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.BoolFlag{Name: "publish-npm", Sources: cli.EnvVars("PUBLISH_NPM")},
			&cli.StringFlag{Name: "container-result", Sources: cli.EnvVars("BUILD_DEV_CONTAINER_RESULT")},
			&cli.StringFlag{Name: "npm-result", Sources: cli.EnvVars("PUBLISH_NPM_DEV_RESULT")},
			&cli.StringFlag{Name: "cargo-sbom-result", Sources: cli.EnvVars("CARGO_SBOM_DEV_RESULT")},
			&cli.StringFlag{Name: "sbom-result", Sources: cli.EnvVars("GENERATE_DEV_SBOMS_RESULT")},
			&cli.StringFlag{Name: "npm-package-name", Sources: cli.EnvVars("NPM_PACKAGE_NAME")},
			&cli.StringFlag{Name: "npm-package-version", Sources: cli.EnvVars("NPM_PACKAGE_VERSION")},
			&cli.StringFlag{Name: "npm-publish-status", Value: "published", Sources: cli.EnvVars("NPM_PUBLISH_STATUS")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			_, err = appsummary.DevPublishStageResult(ctx, d.OutputSink, d.ManifestSink, appsummary.DevPublishStageInput{
				ProjectType:       projecttype.Type(cmd.String("project-type")),
				PublishNPM:        cmd.Bool("publish-npm"),
				ContainerResult:   cmd.String("container-result"),
				NPMResult:         cmd.String("npm-result"),
				CargoSBOMResult:   cmd.String("cargo-sbom-result"),
				SBOMResult:        cmd.String("sbom-result"),
				NPMPackageName:    cmd.String("npm-package-name"),
				NPMPackageVersion: cmd.String("npm-package-version"),
				NPMPublishStatus:  cmd.String("npm-publish-status"),
			})
			return err
		},
	}
}
