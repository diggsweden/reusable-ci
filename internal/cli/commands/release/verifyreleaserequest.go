// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
)

// verifyRequestCmd is the Go port of forgejo-ci's verify-release-request.sh:
// verify that a stable release-request/vX.Y.Z tag on origin authorises the
// final vX.Y.Z tag and is SSH-signed by an allowed signer.
func verifyRequestCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-request",
		Usage: "validate an SSH-signed release-request tag authorizes creating a final release tag",
		Description: `EXAMPLE:
   reusable-ci release validate-request --release-request release-request/v1.2.3 --tag v1.2.3 --allowed-signers-file .forgejo/release-request.allowed_signers`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release-request", Required: true, Sources: cli.EnvVars("RELEASE_REQUEST"), Usage: "release request tag (release-request/vMAJOR.MINOR.PATCH)"},
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cienv.Tag(), Usage: "final stable release tag (vMAJOR.MINOR.PATCH)"},
			&cli.StringFlag{Name: "allowed-signers-file", Required: true, Sources: cli.EnvVars("RELEASE_REQUEST_ALLOWED_SIGNERS"), Usage: "OpenSSH allowed_signers file for release-request SSH signatures"},
			&cli.StringFlag{Name: "remote", Value: "origin", Usage: "git remote queried for request/final tags"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.VerifyReleaseRequest(ctx, git.New(), os.Stdout, apprelease.VerifyReleaseRequestInput{
				ReleaseRequest:     cmd.String("release-request"),
				ReleaseTag:         cmd.String(flagTag),
				AllowedSignersPath: cmd.String("allowed-signers-file"),
				Remote:             cmd.String("remote"),
			})
		},
	}
}
