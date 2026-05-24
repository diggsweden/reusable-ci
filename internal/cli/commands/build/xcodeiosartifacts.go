// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
)

func xcodeIOSListArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "list-artifacts",
		Usage: "list *.ipa / *.xcarchive files under build/",
		Action: func(_ context.Context, _ *cli.Command) error {
			return appbuild.XcodeListBuiltArtifacts(os.Stderr)
		},
	}
}
