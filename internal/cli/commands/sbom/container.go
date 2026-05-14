// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

import (
	"cmp"
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/internal/adapters/syft"
	appsbom "github.com/diggsweden/reusable-ci/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func generateContainerCmd() *cli.Command {
	return &cli.Command{
		Name:  "generate-container",
		Usage: "generate the analyzed-container SBOM for a multi-artifact container release",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-types", Sources: cli.EnvVars("ARTIFACT_TYPES")},
			&cli.StringFlag{Name: "ref-name", Sources: cli.EnvVars("CI_REF_NAME", "GITHUB_REF_NAME")},
			&cli.StringFlag{Name: "repo", Sources: cli.EnvVars("CI_REPO", "GITHUB_REPOSITORY")},
			&cli.StringFlag{Name: "image-name", Sources: cli.EnvVars("IMAGE_NAME")},
			&cli.StringFlag{Name: "image-digest", Sources: cli.EnvVars("IMAGE_DIGEST")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			refName := cmd.String("ref-name")
			if refName == "" {
				refName = cmp.Or(os.Getenv("CI_REF_NAME"), os.Getenv("GITHUB_REF_NAME"))
			}
			repo := cmd.String("repo")
			if repo == "" {
				repo = cmp.Or(os.Getenv("CI_REPO"), os.Getenv("GITHUB_REPOSITORY"))
			}
			return appsbom.GenerateContainer(ctx, syft.New(), maven.New(), git.New(), os.Stdout, os.Stderr, appsbom.GenerateContainerInput{
				ArtifactTypes: cmd.String("artifact-types"),
				RefName:       refName,
				Repo:          repo,
				ImageName:     cmd.String("image-name"),
				ImageDigest:   cmd.String("image-digest"),
			})
		},
	}
}

func findContainerSBOMCmd() *cli.Command {
	return &cli.Command{
		Name:  "find-container-sbom",
		Usage: "find a *-analyzed-container-sbom.spdx.json file in cwd, emit sbom-file=<basename>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			annot := deps.Annotator(cmd)
			return appsbom.FindContainerSBOM(ctx, d.OutputSink, os.Stdout, os.Stderr, annot, appsbom.FindContainerSBOMInput{})
		},
	}
}
