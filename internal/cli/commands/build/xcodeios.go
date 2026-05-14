// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func xcodeIOSCmd() *cli.Command {
	return &cli.Command{
		Name:  "xcode-ios",
		Usage: "xcode-ios build pipeline (macOS-only; the runtime image bundles Go for `go install` builds)",
		Commands: []*cli.Command{
			xcodeIOSVersionInfoCmd(),
			xcodeIOSSetupCodeSigningCmd(),
			xcodeIOSArchiveCmd(),
			xcodeIOSExportIPACmd(),
			xcodeIOSListArtifactsCmd(),
		},
	}
}

func xcodeIOSVersionInfoCmd() *cli.Command {
	return &cli.Command{
		Name:      "version-info",
		Usage:     "extract MARKETING_VERSION / CURRENT_PROJECT_VERSION from project.pbxproj",
		ArgsUsage: "[project] [workspace]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appbuild.XcodeVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.XcodeVersionInfoInput{
				Project:   cmd.Args().Get(0),
				Workspace: cmd.Args().Get(1),
			})
		},
	}
}
