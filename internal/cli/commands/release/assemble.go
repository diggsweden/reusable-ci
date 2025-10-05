// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func assembleCmd() *cli.Command {
	return &cli.Command{
		Name:  "assemble",
		Usage: "stage the canonical release file set and write release-assembly.json",
		Description: `CONFIG_PLAN_JSON is produced by ` + "`config parse-artifacts`" + `.

EXAMPLE:
   CONFIG_PLAN_JSON="$(reusable-ci config parse-artifacts)" \
   reusable-ci release assemble --project-name app --version 1.2.3 --release-artifacts-dir dist`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Required: true, Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config plan JSON emitted by config parse-artifacts"},
			&cli.StringFlag{Name: flagArtifactTransferPlanJSON, Required: true, Sources: cli.EnvVars("ARTIFACT_TRANSFER_PLAN_JSON"), Usage: "typed artifact-transfer plan JSON used by release download-artifacts"},
			&cli.StringFlag{Name: flagAttachArtifacts, Sources: cli.EnvVars("ATTACH_ARTIFACTS"), Usage: "comma/newline-separated workspace-relative extra release asset globs"},
			&cli.StringFlag{Name: "project-name", Required: true, Sources: cli.EnvVars("PROJECT_NAME"), Usage: "project slug used for computed release asset names"},
			&cli.StringFlag{Name: flagVersion, Required: true, Sources: cli.EnvVars("VERSION"), Usage: "release version used for computed release asset names"},
			&cli.StringFlag{Name: flagOutput, Value: domainrelease.DefaultAssemblyFile, Sources: cli.EnvVars("RELEASE_ASSEMBLY"), Usage: "assembly manifest path"},
			&cli.StringFlag{Name: "release-files-dir", Value: domainrelease.DefaultReleaseFilesDir, Sources: cli.EnvVars("RELEASE_FILES_DIR"), Usage: "staging directory for final release files"},
			&cli.StringFlag{Name: flagReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR"), Usage: "downloaded release artifact directory"},
			&cli.StringFlag{Name: flagSBOMDir, Sources: cli.EnvVars("SBOM_DIR"), Usage: "downloaded analyzed-container SBOM directory"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, err := apprelease.Assemble(os.Stderr, apprelease.AssembleInput{
				ConfigPlanJSON:           cmd.String("config-plan-json"),
				ArtifactTransferPlanJSON: cmd.String(flagArtifactTransferPlanJSON),
				AttachArtifacts:          cmd.String(flagAttachArtifacts),
				ProjectName:              cmd.String("project-name"),
				Version:                  cmd.String(flagVersion),
				OutputFile:               cmd.String(flagOutput),
				ReleaseFilesDir:          cmd.String("release-files-dir"),
				ReleaseArtifactsDir:      cmd.String(flagReleaseArtifactsDir),
				SBOMDir:                  cmd.String(flagSBOMDir),
			})

			return err
		},
	}
}
