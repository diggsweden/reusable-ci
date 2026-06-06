// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func xcodeIOSCmd() *cli.Command {
	return &cli.Command{
		Name:  "xcode-ios",
		Usage: "xcode-ios build pipeline (macOS-only; workflows install reusable-ci on the macOS host)",
		Commands: []*cli.Command{
			xcodeIOSArtifactNameCmd(),
			xcodeIOSVersionInfoCmd(),
			xcodeIOSSetupXCConfigCmd(),
			xcodeIOSSetupCodeSigningCmd(),
			xcodeIOSArchiveCmd(),
			xcodeIOSExportIPACmd(),
			xcodeIOSListArtifactsCmd(),
		},
	}
}

func xcodeIOSArtifactNameCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifact-name", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "compute and emit the IPA artifact name",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-name", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit name override; overrides repo/tag heuristics"},
			&cli.StringFlag{Name: "repository-name", Sources: cli.EnvVars("REPOSITORY_NAME"), Usage: "repository basename used to derive the default name"},
			&cli.BoolFlag{Name: "include-tag", Sources: cli.EnvVars("INCLUDE_TAG"), Usage: "append the tag to the artifact name"},
			&cli.StringFlag{Name: "ref-name", Sources: cli.EnvVars("REF_NAME", "GITHUB_REF_NAME"), Usage: "ref/tag appended when --include-tag is set"}, //nolint:goconst // flag name reused across sibling subcommands.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.XcodeArtifactName(ctx, d.OutputSink, os.Stderr, appbuild.XcodeArtifactNameInput{
					ArtifactName:   cmd.String("artifact-name"),
					RepositoryName: cmd.String("repository-name"),
					IncludeTag:     cmd.Bool("include-tag"),
					RefName:        cmd.String("ref-name"),
				})
			})
		},
	}
}

func xcodeIOSVersionInfoCmd() *cli.Command {
	return &cli.Command{
		Name:  "version-info",
		Usage: "extract MARKETING_VERSION / CURRENT_PROJECT_VERSION from project.pbxproj",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "project",
				Sources: cli.EnvVars("PROJECT_PATH"),
				Usage:   "path to the .xcodeproj (defaults to a discovered one in working-dir)",
			},
			&cli.StringFlag{
				Name:    "workspace",
				Sources: cli.EnvVars("WORKSPACE_PATH"),
				Usage:   "path to the .xcworkspace (when the project is part of one)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.XcodeVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.XcodeVersionInfoInput{
					Project:   cmd.String("project"),
					Workspace: cmd.String("workspace"),
				})
			})
		},
	}
}

func xcodeIOSSetupXCConfigCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-xcconfig",
		Usage: "decode optional XCCONFIG_BASE64 and emit xcconfig-path",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "base64", Sources: cli.EnvVars("XCCONFIG_BASE64"), Usage: "base64-encoded .xcconfig body; empty value is a no-op"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "temp-dir", Sources: cli.EnvVars("RUNNER_TEMP", "CI_TEMP_DIR"), Usage: "directory the decoded .xcconfig is written to"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.XcodeXCConfig(ctx, d.OutputSink, annot, appbuild.XcodeXCConfigInput{
					Base64:  cmd.String("base64"),
					TempDir: cmd.String("temp-dir"),
				})
			})
		},
	}
}
