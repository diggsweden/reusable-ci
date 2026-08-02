// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
)

func filesGroup() *cli.Command {
	return &cli.Command{
		Name:  "files",
		Usage: "release file-set manifest helpers",
		Commands: []*cli.Command{
			filesCollectCmd(),
			filesManifestCmd(),
			filesValidateCmd(),
			filesListCmd(),
			filesChecksumFileCmd(),
			filesValidateChecksumsCmd(),
		},
	}
}

func releaseFilesFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: flagDistDir, Value: defaultDistDir, Usage: "dist directory containing GoReleaser artifacts.json and release files"},
		&cli.StringFlag{Name: flagManifest, Value: apprelease.DefaultReleaseFilesManifest, Sources: cli.EnvVars("RELEASE_FILES_MANIFEST"), Usage: "release file manifest path"},
	}
}

func releaseFilesInput(cmd *cli.Command) apprelease.FilesInput {
	return apprelease.FilesInput{
		DistDir:      cmd.String(flagDistDir),
		ManifestFile: cmd.String(flagManifest),
	}
}

func filesCollectCmd() *cli.Command {
	flags := append(releaseFilesFlags(), &cli.StringFlag{Name: flagOutput, Value: cliio.StdSentinel, Usage: "output JSON file ('-' for stdout)"})

	return &cli.Command{
		Name:  "collect",
		Usage: "collect publishable release assets from GoReleaser metadata and signed sidecars",
		Flags: flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			assets, err := apprelease.CollectReleaseAssets(releaseFilesInput(cmd))
			if err != nil {
				return err
			}

			return writeJSON(cmd.String(flagOutput), assets)
		},
	}
}

func filesManifestCmd() *cli.Command {
	flags := append(releaseFilesFlags(),
		&cli.StringFlag{Name: flagOutput, Usage: "output manifest file (defaults to --manifest; '-' for stdout)"},
		&cli.StringFlag{Name: "assets-json", Usage: "release files collect JSON to reuse instead of re-collecting ('-' for stdin)"},
	)

	return &cli.Command{
		Name:  "manifest",
		Usage: "write the versioned release file manifest",
		Flags: flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			input := apprelease.WriteReleaseFileManifestInput{
				FilesInput:     releaseFilesInput(cmd),
				OutputFile:     cmd.String(flagOutput),
				AssetsJSONFile: cmd.String("assets-json"),
			}

			_, err := apprelease.WriteReleaseFileManifest(input)

			return err
		},
	}
}

func filesValidateCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "validate the release file manifest",
		Flags: releaseFilesFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			if err := apprelease.ValidateReleaseFileManifest(releaseFilesInput(cmd)); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "Release file manifest validated: %s\n", cmd.String(flagManifest))

			return nil
		},
	}
}

func filesListCmd() *cli.Command {
	flags := append(releaseFilesFlags(), &cli.StringFlag{Name: "section", Value: "assets", Usage: "manifest section to list: assets, checksums, sboms, evidence, provenance"})

	return &cli.Command{
		Name:  "list",
		Usage: "print release file paths for a manifest section, falling back to discovery when no manifest exists",
		Flags: flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			paths, err := apprelease.ListReleaseFiles(apprelease.ListReleaseFilesInput{
				FilesInput: releaseFilesInput(cmd),
				Section:    cmd.String("section"),
			})
			if err != nil {
				return err
			}

			if len(paths) == 0 {
				return nil
			}

			_, _ = fmt.Fprintln(os.Stdout, strings.Join(paths, "\n"))

			return nil
		},
	}
}

func filesChecksumFileCmd() *cli.Command {
	return &cli.Command{
		Name:  "checksum-file",
		Usage: "print the single release checksums file path",
		Flags: releaseFilesFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			path, err := apprelease.FindReleaseChecksumFile(releaseFilesInput(cmd))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, path)

			return nil
		},
	}
}

func filesValidateChecksumsCmd() *cli.Command {
	flags := append(releaseFilesFlags(), &cli.StringFlag{Name: flagChecksumFile, Sources: cli.EnvVars("CHECKSUM_FILE"), Usage: "checksums file to validate (defaults to release files checksum-file)"})

	return &cli.Command{
		Name:  "validate-checksums",
		Usage: "validate that the checksums file names every public release asset exactly once",
		Flags: flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			return apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{
				FilesInput:    releaseFilesInput(cmd),
				ChecksumsFile: cmd.String(flagChecksumFile),
			})
		},
	}
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	body = append(body, '\n')

	if path != cliio.StdSentinel {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // public release metadata under workspace.
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	return cliio.WriteFile(path, body, 0o644) //nolint:gosec // public release metadata.
}
