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
			&cli.StringFlag{Name: "export-options-base64", Sources: cli.EnvVars("EXPORT_OPTIONS_BASE64"), Usage: "base64-encoded export-options.plist body"},
			&cli.StringFlag{Name: "export-options-var", Value: "EXPORT_OPTIONS_BASE64", Sources: cli.EnvVars("EXPORT_OPTIONS_VAR"), Usage: "env var name shown in error messages when --export-options-base64 is empty"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.XcodeExportIPA(ctx, xcode.NewBuild(), os.Stderr, os.Stderr, appbuild.XcodeExportIPAInput{
				ExportOptionsBase64: cmd.String("export-options-base64"),
				ExportOptionsVar:    cmd.String("export-options-var"),
			})
		},
	}
}
