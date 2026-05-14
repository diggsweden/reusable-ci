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

func xcodeIOSExportIPACmd() *cli.Command {
	return &cli.Command{
		Name:  "export-ipa",
		Usage: "decode the export-options plist and run xcodebuild -exportArchive",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "export-options-base64", Sources: cli.EnvVars("EXPORT_OPTIONS_BASE64")},
			&cli.StringFlag{Name: "export-options-var", Value: "EXPORT_OPTIONS_BASE64", Sources: cli.EnvVars("EXPORT_OPTIONS_VAR")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.XcodeExportIPA(ctx, xcode.NewXcodeBuild(), os.Stdout, os.Stderr, appbuild.XcodeExportIPAInput{
				ExportOptionsBase64: cmd.String("export-options-base64"),
				ExportOptionsVar:    cmd.String("export-options-var"),
			})
		},
	}
}
