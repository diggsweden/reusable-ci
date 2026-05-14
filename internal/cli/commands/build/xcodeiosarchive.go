// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/xcode"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
)

func xcodeIOSArchiveCmd() *cli.Command {
	return &cli.Command{
		Name:  "archive",
		Usage: "run xcodebuild archive against build/app.xcarchive",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "workspace", Sources: cli.EnvVars("WORKSPACE")},
			&cli.StringFlag{Name: "project", Sources: cli.EnvVars("PROJECT")},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME")},
			&cli.StringFlag{Name: "configuration", Sources: cli.EnvVars("CONFIGURATION")},
			&cli.StringFlag{Name: "destination", Sources: cli.EnvVars("DESTINATION")},
			&cli.StringFlag{Name: "xcconfig-path", Sources: cli.EnvVars("XC_CONFIG_PATH")},
			&cli.StringFlag{Name: "build-number", Sources: cli.EnvVars("BUILD_NUMBER")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.XcodeArchive(ctx, xcode.NewXcodeBuild(), os.Stdout, os.Stderr, appbuild.XcodeArchiveInput{
				Workspace:     cmd.String("workspace"),
				Project:       cmd.String("project"),
				Scheme:        cmd.String("scheme"),
				Configuration: cmd.String("configuration"),
				Destination:   cmd.String("destination"),
				XcconfigPath:  cmd.String("xcconfig-path"),
				BuildNumber:   cmd.String("build-number"),
			})
		},
	}
}
