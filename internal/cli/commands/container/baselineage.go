// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

func baseLineagePredicateCmd() *cli.Command {
	return &cli.Command{
		Name:  "base-lineage-predicate",
		Usage: "emit the SLSA Provenance v1.0 predicate for a base-image lineage attestation",
		Description: `Emits the bare SLSA Provenance v1.0 predicate JSON used by
cosign attest --type slsaprovenance1. The OCI image subject is bound by cosign;
the predicate records source, workflow, flavor, base_input_id, image, and the
source commit dependency.

EXAMPLE:
   reusable-ci container base-lineage-predicate --source codeberg.org/org/app --commit <sha> --workflow container-bases.yml --flavor rust --base-input-id <sha256> --image codeberg.org/org/app-base@sha256:<digest> --build-type https://codeberg.org/itiquette/forgejo-ci/container-build/v1`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagSource, Sources: cli.EnvVars("SOURCE_REPOSITORY"), Usage: "source repository URL without git+ prefix (server/owner/repo)"},
			&cli.StringFlag{Name: "commit", Sources: cli.EnvVars("SOURCE_SHA"), Usage: "source commit SHA"},
			&cli.StringFlag{Name: "workflow", Sources: cli.EnvVars("CALLER_WORKFLOW"), Usage: "workflow file that produced the image"},
			&cli.StringFlag{Name: flagFlavor, Sources: cli.EnvVars("BUILD_FLAVOR"), Usage: "base-image flavor"},
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "sha256 content id of the base inputs"},
			&cli.StringFlag{Name: flagImage, Sources: cli.EnvVars("IMAGE_REF"), Usage: "base image digest reference being attested"},
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("BUILD_TYPE"), Usage: "SLSA buildType URI"},
			&cli.StringFlag{Name: "builder-id", Sources: cli.EnvVars("BUILDER_ID"), Usage: "override builder.id; defaults to <source>/.forgejo/workflows/<workflow>@<commit>"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appcontainer.BaseLineagePredicate(os.Stdout, provenance.BaseLineageInput{
				Source:      cmd.String(flagSource),
				Commit:      cmd.String("commit"),
				Workflow:    cmd.String("workflow"),
				Flavor:      cmd.String(flagFlavor),
				BaseInputID: cmd.String(flagBaseInputID),
				Image:       cmd.String(flagImage),
				BuildType:   cmd.String("build-type"),
				BuilderID:   cmd.String("builder-id"),
			})
		},
	}
}
