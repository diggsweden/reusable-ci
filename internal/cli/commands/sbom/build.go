// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// buildCmd is the build-layer GENERATE tier of the SBOM flow: it runs the
// ecosystem's native CycloneDX tool to emit the build-layer bom.json. It lives
// under `sbom` (not `build`) so all SBOM concerns share one surface — `build`
// only builds artifacts. It runs on a per-ecosystem toolchain runner (the
// generate tier of sbom-go.yml / sbom-cargo.yml); the toolchain-free
// `sbom assemble` then harvests the bom.json this produces.
func buildCmd() *cli.Command {
	return &cli.Command{
		Name:  "build",
		Usage: "generate the build-layer SBOM with the language-native tool (go/cargo)",
		Description: `Runs the ecosystem's native CycloneDX tool — cyclonedx-gomod (go) or
   cargo-cyclonedx (cargo) — to produce the build-layer bom.json. These read the
   lockfile, so no compiled artifact is needed. maven/gradle/npm emit the build
   BOM as a build byproduct of ` + "`build <eco> run`" + `, so they are not generated
   here. The toolchain-free ` + "`sbom assemble`" + ` harvests what this produces (or, on
   a runner that has the toolchain, generates it inline itself).

EXAMPLE:
   reusable-ci sbom build --project-type go --name myapp --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Required: true, Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "ecosystem: \"go\" or \"cargo\" (lockfile-read; maven/gradle/npm emit the BOM during the build)"},
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the project manifest/lockfile"},
			&cli.StringFlag{Name: "name", Sources: cli.EnvVars("ARTIFACT_NAME", "BINARY_NAME"), Usage: "artifact name used in the SBOM output path (defaults to $ARTIFACT_NAME, then $BINARY_NAME)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectType := projecttype.Type(cmd.String("project-type"))

			err := buildSBOMGenerator{}.GenerateBuildSBOM(ctx, projectType, cmd.String(flagWorkingDir), cmd.String("name"), os.Stderr)
			if errors.Is(err, errs.ErrUnsupported) {
				return fmt.Errorf("project-type %q has no standalone build-SBOM generator — its build BOM is a byproduct of `build %s run`: %w", projectType, projectType, errs.ErrUsage)
			}

			return err
		},
	}
}
