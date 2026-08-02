// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// isolationCmd wires `reusable-ci validate isolation` — a static SLSA Build L3
// gate over a consumer's release workflow.
func isolationCmd() *cli.Command {
	return &cli.Command{
		Name:  "isolation",
		Usage: "assert SLSA Build L3 job isolation (build job has no signing secrets; checkouts don't persist credentials)",
		Description: `Static SLSA Build L3 gate: the artifact-producing build job must have no
access to signing secrets (those belong only to the separately-trusted signing
job), and every actions/checkout step must set persist-credentials: false.

EXAMPLE:
   reusable-ci validate isolation --workflow .github/workflows/release.yml \
     --build-job build --signing-secret RELEASE_GPG_PRIVATE_KEY --signing-secret COSIGN_PRIVATE_KEY`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "workflow", Required: true, Usage: "path to the workflow file to check"},
			&cli.StringFlag{Name: "build-job", Value: "build", Usage: "the artifact-producing job that must not see signing secrets"},
			&cli.StringSliceFlag{
				Name:  "signing-secret",
				Usage: "signing secret name forbidden in the build job (repeatable)",
				Value: []string{"RELEASE_GPG_PRIVATE_KEY", "RELEASE_GPG_PASSPHRASE", "COSIGN_PRIVATE_KEY", "COSIGN_PASSWORD"},
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.Isolation(os.Stderr, deps.Annotator(cmd), appvalidate.IsolationInput{
				Workflow:       cmd.String("workflow"),
				BuildJob:       cmd.String("build-job"),
				SigningSecrets: cmd.StringSlice("signing-secret"),
			})
		},
	}
}
