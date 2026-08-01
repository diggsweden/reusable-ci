// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/npm"
	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
)

func npmCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm",
		Usage: "npm publish pre-flight checks",
		Commands: []*cli.Command{
			npmValidateTarballCmd(),
			npmValidateVersionCmd(),
			npmFindTarballCmd(),
			npmWriteNPMRCCmd(),
		},
	}
}

func npmWriteNPMRCCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-npmrc",
		Usage: "compose a .npmrc with registry + scope, emitting the literal ${NODE_AUTH_TOKEN} placeholder",
		Description: `EXAMPLE:
   reusable-ci publish npm write-npmrc --registry https://registry.npmjs.org --scope @examplescope --output .npmrc`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "registry", Required: true, Sources: cli.EnvVars("REGISTRY", "NPM_REGISTRY"), Usage: "npm registry URL written into the .npmrc"},
			&cli.StringFlag{Name: "scope", Sources: cli.EnvVars("SCOPE", "PACKAGE_SCOPE"), Usage: "package scope (e.g. @examplescope) routed to the registry"},
			&cli.StringFlag{Name: "output", Sources: cli.EnvVars("NPMRC_OUTPUT"), Usage: "destination path; '-' writes to stdout (default: stdout)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			path := cmd.String("output")
			if path == "" {
				path = cliio.StdSentinel
			}

			w, err := cliio.CreateWriter(path, 0o600) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				return err
			}

			defer func() { _ = w.Close() }()

			return apppublish.WriteNPMRC(apppublish.NPMRCInput{
				Registry: cmd.String("registry"),
				Scope:    cmd.String("scope"),
				Output:   w,
			})
		},
	}
}

func npmFindTarballCmd() *cli.Command {
	return &cli.Command{
		Name:  "find-tarball",
		Usage: "find a top-level npm tarball and emit tarball",
		Description: `EXAMPLE:
   reusable-ci publish npm find-tarball --working-dir .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory scanned for the npm tarball (where 'npm pack' ran)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)
				_, err := apppublish.FindArtifact(ctx, d.OutputSink, os.Stderr, annot, apppublish.FindArtifactInput{
					Dir:       cmd.String("working-dir"),
					Exts:      []string{".tgz", ".tar.gz"},
					OutputKey: "tarball",
					Label:     "npm tarball",
				})

				return err
			})
		},
	}
}

func npmValidateVersionCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-version",
		Usage: "validate that package@version is unpublished (fails if the version already exists in the registry)",
		Description: `EXAMPLE:
   reusable-ci publish npm validate-version --name @examplescope/app --version 1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing package.json (used when --name is omitted)"},
			&cli.StringFlag{Name: "name", Sources: cli.EnvVars("PACKAGE_NAME"), Usage: "package name (defaults to the value in package.json)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "package version to check against the registry"},
			&cli.StringFlag{Name: "registry", Sources: cli.EnvVars("NPM_REGISTRY"), Usage: "npm registry to query"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return apppublish.NPMCheckVersion(ctx, npm.New(), d.OutputSink, os.Stderr, annot, apppublish.NPMCheckVersionInput{
					Dir:      cmd.String("working-dir"),
					Name:     cmd.String("name"),
					Version:  cmd.String("version"),
					Registry: cmd.String("registry"),
				})
			})
		},
	}
}

func npmValidateTarballCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-tarball",
		Usage: "extract the npm tarball produced by `npm pack`, verify dist/cli.js is present",
		Description: `EXAMPLE:
   # Run in the directory holding the packed tarball (takes no flags)
   reusable-ci publish npm validate-tarball`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return apppublish.NPMValidateTarball(ctx, os.Stderr, os.Stderr, annot, apppublish.NPMValidateTarballInput{})
		},
	}
}
