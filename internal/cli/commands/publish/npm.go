// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

func npmCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm",
		Usage: "npm publish pre-flight checks",
		Commands: []*cli.Command{
			npmValidateTarballCmd(),
		},
	}
}

func npmValidateTarballCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-tarball",
		Usage: "extract the npm tarball produced by `npm pack`, verify dist/cli.js is present",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			return apppublish.NPMValidateTarball(ctx, os.Stdout, os.Stderr, annot, apppublish.NPMValidateTarballInput{})
		},
	}
}

func validateAuthCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-auth",
		Usage: "validate registry authentication configuration before publishing",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "use-ci-token", Sources: cli.EnvVars("USE_CI_TOKEN")},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("TARGET_REGISTRY")},
			&cli.StringFlag{Name: "expected-registry", Value: "ghcr.io", Sources: cli.EnvVars("CI_REGISTRY")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			// Match the bash: presence-check on $REGISTRY_PASSWORD via env
			// (the value never reaches argv).
			hasPassword := os.Getenv("REGISTRY_PASSWORD") != ""
			return apppublish.RegistryAuth(ctx, os.Stdout, os.Stderr, annot, publish.RegistryAuthInput{
				UseCIToken:       cmd.Bool("use-ci-token"),
				Registry:         cmd.String("registry"),
				ExpectedRegistry: cmd.String("expected-registry"),
				HasPassword:      hasPassword,
			})
		},
	}
}
