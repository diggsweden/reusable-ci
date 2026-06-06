// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
			&cli.StringFlag{Name: "workspace", Sources: cli.EnvVars("WORKSPACE"), Usage: "path to the .xcworkspace (mutually exclusive with --project)"},
			&cli.StringFlag{Name: "project", Sources: cli.EnvVars("PROJECT"), Usage: "path to the .xcodeproj (mutually exclusive with --workspace)"},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME"), Usage: "Xcode scheme to archive"},
			&cli.StringFlag{Name: "configuration", Sources: cli.EnvVars("CONFIGURATION"), Usage: "Xcode build configuration (Release/Debug/…)"},
			&cli.StringFlag{Name: "destination", Sources: cli.EnvVars("DESTINATION"), Usage: "xcodebuild destination spec (e.g. generic/platform=iOS)"},
			&cli.StringFlag{Name: "xcconfig-path", Sources: cli.EnvVars("XC_CONFIG_PATH"), Usage: "optional .xcconfig file passed via -xcconfig"},
			&cli.StringFlag{Name: "build-number", Sources: cli.EnvVars("BUILD_NUMBER"), Usage: "CURRENT_PROJECT_VERSION override (forwarded via xcodebuild)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.XcodeArchive(ctx, xcode.NewBuild(), os.Stderr, os.Stderr, appbuild.XcodeArchiveInput{
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
