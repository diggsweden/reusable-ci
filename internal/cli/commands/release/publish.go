// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/osfs"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

const (
	strategyReconcile = "reconcile"
	strategyRecreate  = "recreate"
)

func publishCmd() *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{Name: "strategy", Value: strategyReconcile, Sources: cli.EnvVars("RELEASE_STRATEGY"), Usage: "reconcile updates the release and its assets in place; recreate deletes and recreates it"},
		&cli.StringFlag{Name: flagTag, Required: true, Sources: cienv.Tag(), Usage: "tag the release is created from (e.g. v1.2.3)"},
		&cli.StringFlag{Name: flagRepository, Required: true, Sources: cienv.Repository(), Usage: "\"owner/repo\" release repository"},
		&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "human-readable release title (defaults to the tag)"},
		&cli.BoolFlag{Name: "release-name-from-repository", Sources: cli.EnvVars("RELEASE_NAME_FROM_REPOSITORY"), Usage: "when --release-name is empty, default to '<repo-name> <tag>' instead of the tag (reconcile only)"},
		&cli.BoolFlag{Name: "draft", Value: true, Sources: cli.EnvVars("RELEASE_DRAFT", "DRAFT"), Usage: "publish/update the release as a draft (default true)"},
		&cli.StringFlag{Name: "release-notes-file", Value: "dist/release-notes.md", Sources: cli.EnvVars("RELEASE_NOTES_FILE"), Usage: "path to the release-notes markdown body"},
		&cli.StringSliceFlag{Name: "asset", Usage: "release asset file to publish (repeatable); defaults to --manifest assets (reconcile only)"},
		// recreate-strategy flags, matching the deprecated `release create`.
		&cli.StringFlag{Name: "make-latest", Value: makeLatestDefault, Sources: cli.EnvVars("MAKE_LATEST"), Usage: "platform latest handling: true, false, or legacy (recreate only)"},
		&cli.StringFlag{Name: flagAttachArtifacts, Sources: cli.EnvVars("ATTACH_ARTIFACTS"), Usage: "comma-separated globs of extra files to attach beyond release-dir (recreate only)"},
		&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "project slug used in computed asset names (recreate only; defaults to repo basename)"},
		&cli.StringFlag{Name: flagChecksumsFile, Value: domainrelease.ChecksumsFile, Sources: cli.EnvVars("CI_CHECKSUMS_FILE"), Usage: "path to the SHA256 manifest to attach (recreate only)"},
		&cli.StringFlag{Name: "release-dir", Value: domainrelease.DefaultReleaseArtifactsDir, Sources: cli.EnvVars("RELEASE_DIR"), Usage: "directory whose files are attached as release assets (recreate only)"},
		&cli.StringFlag{Name: flagAssembly, Sources: cli.EnvVars("RELEASE_ASSEMBLY"), Usage: "release assembly manifest to upload exactly (recreate only)"},
	}, releaseFilesFlags()...)

	return &cli.Command{
		Name:  "publish",
		Usage: "create or update a release and its assets on the detected platform",
		Description: `The default --strategy=reconcile creates the release if missing and
updates it and its assets in place. --strategy=recreate deletes any
existing release first and recreates it with the computed asset set.

EXAMPLES:
   # Publish assets listed in the release file manifest
   reusable-ci release publish --tag=v1.2.3 --repository=examplescope/myapp \
       --release-name="myapp v1.2.3" --manifest=dist/release-files.json

   # Publish an explicit asset list instead of reading the manifest
   reusable-ci release publish --tag=v1.2.3 --repository=examplescope/myapp \
       --release-notes-file=dist/release-notes.md --asset=dist/myapp.tar.gz --asset=dist/checksums.txt

   # Delete-and-recreate with assets from an assembly manifest
   reusable-ci release publish --strategy=recreate --tag=v1.2.3 \
       --repository=examplescope/myapp --assembly=.reusable-ci/release-assembly.json`,
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			switch cmd.String("strategy") {
			case strategyReconcile:
				return runPublishReconcile(ctx, cmd)
			case strategyRecreate:
				return runPublishRecreate(ctx, cmd)
			default:
				return fmt.Errorf("unknown --strategy %q (want %s or %s): %w",
					cmd.String("strategy"), strategyReconcile, strategyRecreate, errs.ErrUsage)
			}
		},
	}
}

func runPublishReconcile(ctx context.Context, cmd *cli.Command) error {
	return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
		publisher, err := d.RequireReleasePublisher()
		if err != nil {
			return err
		}

		assets := cmd.StringSlice("asset")
		if len(assets) == 0 {
			assets, err = apprelease.ListReleaseFiles(apprelease.ListReleaseFilesInput{
				FilesInput: releaseFilesInput(cmd),
				Section:    "assets",
			})
			if err != nil {
				return err
			}
		}

		return apprelease.PublishRelease(ctx, publisher, os.Stderr, apprelease.PublishReleaseInput{
			Tag:                       cmd.String(flagTag),
			Repository:                cmd.String(flagRepository),
			ReleaseName:               cmd.String("release-name"),
			ReleaseNameFromRepository: cmd.Bool("release-name-from-repository"),
			ReleaseNotesFile:          cmd.String("release-notes-file"),
			Draft:                     cmd.Bool("draft"),
			Assets:                    assets,
		})
	})
}

// runPublishRecreate is the delete-and-recreate strategy: remove any
// existing release for the tag and recreate it with the computed assets.
func runPublishRecreate(ctx context.Context, cmd *cli.Command) error {
	return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
		creator, err := d.RequireReleaseCreator()
		if err != nil {
			return err
		}

		return apprelease.CreateRelease(ctx, creator, osfs.New(), os.Stderr, apprelease.CreateReleaseInput{
			Tag:              cmd.String(flagTag),
			Repository:       cmd.String(flagRepository),
			ReleaseName:      cmd.String("release-name"),
			Draft:            cmd.Bool("draft"),
			MakeLatest:       cmd.String("make-latest"),
			AttachArtifacts:  cmd.String(flagAttachArtifacts),
			ReleaseNotesFile: cmd.String("release-notes-file"),
			ArtifactName:     cmd.String(flagArtifactName),
			ChecksumsFile:    cmd.String(flagChecksumsFile),
			ReleaseDir:       cmd.String("release-dir"),
			AssemblyFile:     cmd.String(flagAssembly),
		})
	})
}
