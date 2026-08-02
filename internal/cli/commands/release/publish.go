// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/osfs"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
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
		dryrun.Flag("forge release mutations (create/update, delete, asset uploads)"),
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
		publisher, err := publishReconcilePublisher(d, dryrun.Enabled(cmd))
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
		creator, err := publishRecreateCreator(d, dryrun.Enabled(cmd))
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

// publishReconcilePublisher picks the forge publisher, or — in dry-run —
// the narrating preview decorator, which needs no forge (mirroring the
// ledger's dry-run registry). App-layer validation and asset/manifest
// resolution run unchanged either way, so config errors still surface.
func publishReconcilePublisher(d *deps.Deps, dryRun bool) (provider.ReleasePublisher, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if dryRun {
		return dryRunReleaseForge{out: os.Stderr}, nil
	}

	return d.RequireReleasePublisher()
}

// publishRecreateCreator is the recreate-strategy counterpart of
// publishReconcilePublisher.
func publishRecreateCreator(d *deps.Deps, dryRun bool) (provider.ReleaseCreator, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if dryRun {
		return dryRunReleaseForge{out: os.Stderr}, nil
	}

	return d.RequireReleaseCreator()
}

// dryRunReleaseForge previews the forge release mutations without a forge
// client: it narrates the create/update (reconcile) or delete-and-recreate
// API calls and each asset upload, then performs nothing — the publish
// counterpart of the ledger's dryRunRegistry decorator. Each asset line
// re-checks existence at narration time so the preview shows exactly what
// a real run would upload.
type dryRunReleaseForge struct {
	out io.Writer
}

func (f dryRunReleaseForge) PublishRelease(_ context.Context, repo string, spec provider.ReleaseSpec) error {
	_, _ = fmt.Fprintf(f.out, "[dry-run] would create or update release %s (%q, draft=%t, prerelease=%t) on %s and reconcile its assets\n",
		spec.Tag, spec.Name, spec.Draft, spec.Prerelease, repo)
	f.narrateAssetUploads(spec.Assets)

	return nil
}

func (f dryRunReleaseForge) CreateRelease(_ context.Context, repo string, spec provider.ReleaseSpec) error {
	_, _ = fmt.Fprintf(f.out, "[dry-run] would delete any existing release for tag %s on %s\n", spec.Tag, repo)
	_, _ = fmt.Fprintf(f.out, "[dry-run] would create release %s (%q, draft=%t, prerelease=%t, make-latest=%s) on %s\n",
		spec.Tag, spec.Name, spec.Draft, spec.Prerelease, spec.MakeLatest, repo)
	f.narrateAssetUploads(spec.Assets)

	return nil
}

func (f dryRunReleaseForge) narrateAssetUploads(assets []string) {
	for _, asset := range assets {
		note := ""
		if info, err := os.Stat(asset); err != nil || !info.Mode().IsRegular() {
			note = " (missing on disk)"
		}

		_, _ = fmt.Fprintf(f.out, "[dry-run] would upload asset %s%s\n", asset, note)
	}
}
