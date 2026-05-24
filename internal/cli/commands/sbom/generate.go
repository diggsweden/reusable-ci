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
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// generateGroup wires `reusable-ci sbom generate <layer>` — pick the
// CISA layer(s) to produce via syft. Each subcommand writes the layer's
// SBOM artefacts to the working directory.
func generateGroup() *cli.Command {
	return &cli.Command{
		Name:  "generate",
		Usage: "generate one or all CISA SBOM layers (artifacts, container, all)",
		Commands: []*cli.Command{
			generateAllCmd(),
			generateArtifactsCmd(),
			generateContainerCmd(),
		},
	}
}

func generateAllCmd() *cli.Command {
	return &cli.Command{
		Name:  "all",
		Usage: "generate every requested CISA layer (build / analyzed-artifact / analyzed-container)",
		Description: `EXAMPLES:
   # Generate the build-level SBOM for an NPM project (default layer)
   reusable-ci sbom generate all --project-type=npm

   # Generate build + analyzed-container layers for a Go service;
   # --container-image is required when the analyzed-container layer is requested.
   reusable-ci sbom generate all --project-type=go \
       --layers=build,analyzed-container \
       --container-image=ghcr.io/diggsweden/example:v1.2.3

   # Bundle the produced layers into a release-attached zip
   reusable-ci sbom generate all --project-type=maven --create-zip`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Value: string(projecttype.Auto), Usage: "ecosystem driving the syft scan (auto/maven/gradle/npm/go/cargo/…)"},
			&cli.StringFlag{Name: "layers", Value: "build", Usage: "comma-separated CISA layers to produce (build,source,analyzed-container,…)"},
			&cli.StringFlag{Name: "version", Usage: "release version embedded in the SBOM filenames"},
			&cli.StringFlag{Name: "name", Usage: "project slug used as the SBOM filename prefix"},
			&cli.StringFlag{Name: "working-dir", Value: ".", Usage: "directory syft scans"},
			&cli.StringFlag{Name: "container-image", Usage: "container image to analyze (required for the analyzed-container layer)"},
			&cli.BoolFlag{Name: "create-zip", Usage: "additionally bundle the layers into a release-attached zip"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appsbom.Generate(ctx, syft.New(), maven.New(), git.New(), os.Stderr, os.Stderr, appsbom.GenerateInput{
				ProjectType:    cmd.String("project-type"),
				Layers:         cmd.String("layers"),
				Version:        cmd.String("version"),
				Name:           cmd.String("name"),
				WorkingDir:     cmd.String("working-dir"),
				ContainerImage: cmd.String("container-image"),
				CreateZip:      cmd.Bool("create-zip"),
			})
		},
	}
}

func generateArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "artifacts",
		Usage: "generate artifact-level SBOMs for every artifact in a JSON artifact plan",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config-plan-json", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON listing per-artifact working directories"},
			&cli.StringFlag{Name: "sboms", Value: "all", Sources: cli.EnvVars("SBOMS"), Usage: "sboms enum (\"all\", \"build,source\", …) selecting which layers to produce"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "release version embedded in the SBOM filenames"},
			&cli.StringFlag{Name: "working-dir", Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "default working directory when no per-artifact override is in the plan"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appsbom.GenerateArtifacts(ctx, syft.New(), maven.New(), git.New(), os.Stderr, os.Stderr, appsbom.GenerateArtifactsInput{
				ConfigPlanJSON: cmd.String("config-plan-json"),
				SBOMs:          cmd.String("sboms"),
				Version:        cmd.String("version"),
				WorkingDir:     cmd.String("working-dir"),
			})
		},
	}
}

func generateContainerCmd() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "generate the analyzed-container SBOM for a multi-artifact container release",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-types", Sources: cli.EnvVars("ARTIFACT_TYPES"), Usage: "comma-separated ecosystems embedded in the multi-artifact container SBOM"},
			&cli.StringFlag{Name: "ref-name", Sources: cli.EnvVars("CI_REF_NAME", "GITHUB_REF_NAME"), Usage: "git ref name (leading \"v\" is stripped) used in the SBOM filename"},
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("CI_REPO", "GITHUB_REPOSITORY"), Usage: "\"owner/repo\" used to derive the project slug"},
			&cli.StringFlag{Name: "image-name", Sources: cli.EnvVars("IMAGE_NAME"), Usage: "fully-qualified image reference syft analyses (e.g. registry/org/app)"},
			&cli.StringFlag{Name: "image-digest", Sources: cli.EnvVars("IMAGE_DIGEST"), Usage: "sha256:… digest pinning the exact image manifest to analyse"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// urfave/cli v3's Sources picks the first env var that
			// EXISTS, so a CI workflow that sets `CI_REF_NAME=""`
			// (literal empty, via `${{ env.X || '' }}`) shadows a
			// non-empty `GITHUB_REF_NAME`. Fall back via cmp.Or so
			// the first non-empty value wins instead.
			refName := cmd.String("ref-name")
			if refName == "" {
				refName = cmp.Or(os.Getenv("CI_REF_NAME"), os.Getenv("GITHUB_REF_NAME"))
			}

			repo := cmd.String("repository")
			if repo == "" {
				repo = cmp.Or(os.Getenv("CI_REPO"), os.Getenv("GITHUB_REPOSITORY"))
			}

			return appsbom.GenerateContainer(ctx, syft.New(), maven.New(), git.New(), os.Stderr, os.Stderr, appsbom.GenerateContainerInput{
				ArtifactTypes: cmd.String("artifact-types"),
				RefName:       refName,
				Repo:          repo,
				ImageName:     cmd.String("image-name"),
				ImageDigest:   cmd.String("image-digest"),
			})
		},
	}
}
