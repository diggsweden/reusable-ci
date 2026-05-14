// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func signCmd() *cli.Command {
	return &cli.Command{
		Name:  "sign",
		Usage: "GPG-detach-sign checksums.sha256 and every release-artifact (.asc files land in cwd)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "gpg-key-id", Required: true, Sources: cli.EnvVars("GPG_KEY_ID")},
			&cli.StringFlag{Name: "checksums-file", Sources: cli.EnvVars("CHECKSUMS_FILE")},
			&cli.StringFlag{Name: "release-artifacts-dir", Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apprelease.SignArtifacts(ctx, gpg.New(), apprelease.SignInput{
				GPGKeyID:            cmd.String("gpg-key-id"),
				ChecksumsFile:       cmd.String("checksums-file"),
				ReleaseArtifactsDir: cmd.String("release-artifacts-dir"),
			}, os.Stdout)
		},
	}
}

func checksumsCmd() *cli.Command {
	return &cli.Command{
		Name:  "checksums",
		Usage: "compute SHA256 over release artefacts, attached patterns, and SBOM layers",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output", Sources: cli.EnvVars("OUTPUT_FILE"),
				Usage: "manifest path (default: checksums.sha256)"},
			&cli.StringFlag{Name: "release-artifacts-dir", Sources: cli.EnvVars("RELEASE_ARTIFACTS_DIR")},
			&cli.StringFlag{Name: "attach-artifacts", Sources: cli.EnvVars("ATTACH_ARTIFACTS"),
				Usage: "comma-separated globs for additional files to checksum (paths kept verbatim)"},
			&cli.StringFlag{Name: "sbom-dir", Sources: cli.EnvVars("SBOM_DIR")},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			n, err := apprelease.Checksums(apprelease.ChecksumsInput{
				OutputFile:          cmd.String("output"),
				ReleaseArtifactsDir: cmd.String("release-artifacts-dir"),
				AttachArtifacts:     cmd.String("attach-artifacts"),
				SBOMDir:             cmd.String("sbom-dir"),
			}, os.Stdout)
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Println("No artefacts found to checksum.")
			}
			return nil
		},
	}
}

func sbomZipCmd() *cli.Command {
	return &cli.Command{
		Name:      "sbom-zip",
		Usage:     "bundle all SBOM layers into <project>-<version>-sboms.zip; optionally GPG-sign",
		ArgsUsage: "<project-name> <version>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "sign", Sources: cli.EnvVars("SIGN_ARTIFACTS"),
				Usage: "additionally sign the resulting zip"},
			&cli.StringFlag{Name: "gpg-key-id", Sources: cli.EnvVars("GPG_KEY_ID"),
				Usage: "key id used when --sign is set"},
			&cli.StringFlag{Name: "sbom-dir", Sources: cli.EnvVars("SBOM_DIR")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args()
			if args.Len() < 2 {
				return fmt.Errorf("Usage: release sbom-zip <project-name> <version>: %w", errs.ErrUsage)
			}
			res, err := apprelease.CreateSBOMZip(ctx, gpg.New(), apprelease.SBOMZipInput{
				ProjectName:   args.Get(0),
				Version:       args.Get(1),
				SBOMDir:       cmd.String("sbom-dir"),
				SignArtifacts: cmd.Bool("sign"),
				GPGKeyID:      cmd.String("gpg-key-id"),
			}, os.Stdout)
			if err != nil {
				return err
			}
			if res.ZipName == "" {
				return nil // no-op, nothing to do
			}
			return nil
		},
	}
}
