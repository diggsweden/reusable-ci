// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package artifact wires `reusable-ci artifact upload|download` — the
// forge-neutral run-artifact store (the actions/upload-artifact and
// actions/download-artifact equivalents). The surface is identical on
// every forge; the detected provider supplies the transport, and a forge
// that cannot perform an operation fails with a typed "not supported"
// rather than silently doing the wrong thing.
package artifact

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appartifact "github.com/diggsweden/reusable-ci/internal/app/artifact"
	"github.com/diggsweden/reusable-ci/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// New returns the `artifact` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "artifact",
		Usage: "upload/download per-run CI artifacts (forge-neutral)",
		Commands: []*cli.Command{
			uploadCmd(),
			downloadCmd(),
		},
	}
}

func uploadCmd() *cli.Command {
	return &cli.Command{
		Name:  "upload",
		Usage: "upload files as a named artifact into the current workflow run",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Required: true, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "logical artifact name"},
			&cli.StringFlag{Name: "dir", Sources: cli.EnvVars("ARTIFACT_DIR"), Usage: "directory whose contents are uploaded (or use --file/--path)"},
			&cli.StringSliceFlag{Name: "file", Sources: cli.EnvVars("ARTIFACT_FILES"), Usage: "explicit file to upload (repeatable, flattened to basename)"},
			&cli.StringSliceFlag{Name: "path", Sources: cli.EnvVars("ARTIFACT_PATHS"), Usage: "glob pattern preserving structure (*, **, [set], !exclude; repeatable or newline-separated) — the upload-artifact path: contract"},
			&cli.IntFlag{Name: "retention-days", Sources: cli.EnvVars("ARTIFACT_RETENTION_DAYS"), Usage: "retention in days (0 = forge default)"},
			&cli.StringFlag{Name: "if-no-files", Value: "error", Sources: cli.EnvVars("ARTIFACT_IF_NO_FILES"), Usage: "when nothing matches: error | warn | ignore"},
			&cli.BoolFlag{Name: "include-hidden", Sources: cli.EnvVars("ARTIFACT_INCLUDE_HIDDEN"), Usage: "include dotfiles/hidden files (off by default, matching upload-artifact)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				up, err := dep.RequireRunArtifactUploader()
				if err != nil {
					return err
				}

				_, err = appartifact.Upload(ctx, up, dep.OutputSink, os.Stderr, provider.RunArtifactUpload{
					Name:          cmd.String("name"),
					Dir:           cmd.String("dir"),
					Files:         cmd.StringSlice("file"),
					Paths:         cmd.StringSlice("path"),
					RetentionDays: cmd.Int("retention-days"),
					IfNoFiles:     provider.IfNoFilesPolicy(cmd.String("if-no-files")),
					IncludeHidden: cmd.Bool("include-hidden"),
				})

				return err
			})
		},
	}
}

func downloadCmd() *cli.Command {
	return &cli.Command{
		Name:  "download",
		Usage: "download a named run artifact into a directory",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "exact artifact name to download (omit with --pattern)"},
			&cli.StringFlag{Name: "pattern", Sources: cli.EnvVars("ARTIFACT_PATTERN"), Usage: "glob over artifact names; downloads every match (alternative to --name)"},
			&cli.BoolFlag{Name: "merge-multiple", Sources: cli.EnvVars("ARTIFACT_MERGE_MULTIPLE"), Usage: "with --pattern: flatten all matches into --dir instead of --dir/<name>/"},
			&cli.StringFlag{Name: "dir", Required: true, Sources: cli.EnvVars("ARTIFACT_DIR"), Usage: "destination directory"},
			&cli.StringFlag{Name: "run-id", Sources: cienv.RunID(), Usage: "run the artifact belongs to (default: current run)"},
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "owner/repo the run belongs to (default: current repo)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				dl, err := dep.RequireRunArtifactDownloader()
				if err != nil {
					return err
				}

				_, err = appartifact.Download(ctx, dl, dep.OutputSink, os.Stderr, provider.RunArtifactDownload{
					Name:          cmd.String("name"),
					Pattern:       cmd.String("pattern"),
					MergeMultiple: cmd.Bool("merge-multiple"),
					Dir:           cmd.String("dir"),
					RunID:         cmd.String("run-id"),
					Repository:    cmd.String("repository"),
				})

				return err
			})
		},
	}
}
