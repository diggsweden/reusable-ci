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

When --sign-job is set, additional Forgejo release-signing invariants are
checked: prepare release identity outputs, signer secret enumeration, dist-digest
handoff wiring, prepare-secret-before-checkout ordering, and cache-free
setup-toolchain use. --single-pin-subject additionally enforces that every
matching cross-repo reference in the workflow directory uses one commit pin.

EXAMPLE:
   reusable-ci validate isolation --workflow .github/workflows/release.yml \
     --build-job build --signing-secret RELEASE_GPG_PRIVATE_KEY --signing-secret COSIGN_PRIVATE_KEY`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkflow, Required: true, Usage: "path to the workflow file to check"},
			&cli.StringFlag{Name: "build-job", Value: "build", Usage: "the artifact-producing job that must not see signing secrets"},
			&cli.StringFlag{Name: "sign-job", Usage: "cross-repo signing workflow call-site job; enables Forgejo release-signing channel checks"},
			&cli.StringFlag{Name: "prepare-job", Usage: "job that emits release-tag/release-sha and may check signing-secret presence before checkout"},
			&cli.StringFlag{Name: "dist-digest-output", Usage: "build job output carrying the dist-digest passed to the signer"},
			&cli.StringFlag{Name: "single-pin-subject", Usage: "subject whose @<sha> refs must agree across the workflow directory (for forgejo-ci: itiquette/forgejo-ci)"},
			&cli.StringSliceFlag{
				Name:  "signing-secret",
				Usage: "signing secret name forbidden in the build job (repeatable)",
				Value: []string{"RELEASE_GPG_PRIVATE_KEY", "RELEASE_GPG_PASSPHRASE", "COSIGN_PRIVATE_KEY", "COSIGN_PASSWORD"},
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.Isolation(os.Stderr, deps.Annotator(cmd), appvalidate.IsolationInput{
				Workflow:         cmd.String(flagWorkflow),
				BuildJob:         cmd.String("build-job"),
				SignJob:          cmd.String("sign-job"),
				PrepareJob:       cmd.String("prepare-job"),
				DistDigestOutput: cmd.String("dist-digest-output"),
				SinglePinSubject: cmd.String("single-pin-subject"),
				SigningSecrets:   cmd.StringSlice("signing-secret"),
			})
		},
	}
}
