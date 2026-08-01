// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func forgePackagesCmd() *cli.Command {
	return &cli.Command{
		Name:  "forge-packages",
		Usage: "publish to the forge-native package registry (GitHub Packages / GitLab Package Registry / Forgejo)",
		Commands: []*cli.Command{
			forgePackagesDeployCmd(),
		},
	}
}

func forgePackagesDeployCmd() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "publish to the detected forge's own package registry (Maven or npm)",
		Description: `Resolves the forge-native registry for the active forge + ecosystem (URL +
   auth), writes a credentialed settings.xml/.npmrc to a 0600 temp file (the
   token never reaches argv), and runs the deploy — one binary-owned sequence
   replacing the per-forge inline mvn/npm. The forge is auto-detected (override
   with --provider). Runs in the current directory; cd into the project first.

   --project-type maven: mvn deploy. --project-type npm: npm publish the *.tgz.

EXAMPLE:
   cd module && reusable-ci publish forge-packages deploy --project-type maven`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Value: "maven", Sources: cli.EnvVars("PROJECT_TYPE"), Usage: "ecosystem: \"maven\" or \"npm\""},
			&cli.StringFlag{Name: "cli-opts", Sources: cli.EnvVars("MAVEN_CLI_OPTS"), Usage: "extra args forwarded to mvn (maven only; whitespace-separated)"},
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory holding the packed *.tgz (npm only)"}, //nolint:goconst // shared flag name across commands.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name for the deps handle.
				switch projectType := cmd.String("project-type"); projectType {
				case "npm":
					resolver, err := d.RequireForgeNPMRegistryResolver()
					if err != nil {
						return err
					}

					return apppublish.ForgePackagesNPMPublish(ctx, npm.New(), resolver, os.Stderr, os.Stderr, apppublish.ForgePackagesNPMPublishInput{
						WorkingDir: cmd.String("working-dir"),
					})
				case "maven", "":
					resolver, err := d.RequireForgeMavenRegistryResolver()
					if err != nil {
						return err
					}

					return apppublish.ForgePackagesDeploy(ctx, maven.New(), resolver, os.Stderr, os.Stderr, apppublish.ForgePackagesDeployInput{
						CLIOpts: strings.Fields(cmd.String("cli-opts")),
					})
				default:
					return fmt.Errorf("--project-type must be \"maven\" or \"npm\", got %q: %w", projectType, errs.ErrUsage)
				}
			})
		},
	}
}
