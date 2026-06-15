// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/npm"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func npmCmd() *cli.Command {
	return &cli.Command{
		Name:  "npm",
		Usage: "NPM build helpers",
		Commands: []*cli.Command{
			npmMetadataCmd(),
			npmApplicationCmd(),
			npmPackCmd(),
		},
	}
}

func npmApplicationCmd() *cli.Command {
	return &cli.Command{
		Name:  "application", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "run `npm run <script>` if package.json declares it; otherwise no-op",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing package.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "script", Value: "build", Usage: "npm script name to run"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.NPMApplication(ctx, npm.New(), os.Stderr, os.Stderr, appbuild.NPMApplicationInput{
				Dir:        cmd.String("working-dir"),
				ScriptName: cmd.String("script"),
			})
		},
	}
}

func npmMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "metadata", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "read package.json metadata and emit CI outputs",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing package.json"},
			&cli.StringFlag{Name: "scope", Sources: cli.EnvVars("SCOPE", "PACKAGE_SCOPE"), Usage: "expected scope (e.g. @diggsweden); errors when package.json disagrees"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.NPMMetadata(ctx, d.OutputSink, os.Stderr, annot, appbuild.NPMMetadataInput{
					Dir:          cmd.String("working-dir"),
					PackageScope: cmd.String("scope"),
				})
			})
		},
	}
}

func npmPackCmd() *cli.Command {
	return &cli.Command{
		Name:  "pack",
		Usage: "run npm pack --json and emit the tarball output",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory containing the package.json 'npm pack' runs against"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.NPMPack(ctx, npm.New(), d.OutputSink, os.Stderr, os.Stderr, appbuild.NPMMetadataInput{Dir: cmd.String("working-dir")})
			})
		},
	}
}
