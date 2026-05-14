// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package sbom wires `reusable-ci sbom <subcmd>`.
package sbom

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/internal/adapters/syft"
	appsbom "github.com/diggsweden/reusable-ci/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// New returns the `sbom` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "sbom",
		Usage: "CISA-layered SBOM generation (SPDX + CycloneDX via syft)",
		Commands: []*cli.Command{
			generateCmd(),
			generateContainerCmd(),
			findContainerSBOMCmd(),
		},
	}
}

func generateCmd() *cli.Command {
	return &cli.Command{
		Name:  "generate",
		Usage: "generate CISA-layered SBOMs (build / analyzed-artifact / analyzed-container)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Value: string(projecttype.Auto)},
			&cli.StringFlag{Name: "layers", Value: "build"},
			&cli.StringFlag{Name: "version"},
			&cli.StringFlag{Name: "name"},
			&cli.StringFlag{Name: "working-dir", Value: "."},
			&cli.StringFlag{Name: "container-image"},
			&cli.BoolFlag{Name: "create-zip"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appsbom.Generate(ctx, syft.New(), maven.New(), git.New(), os.Stdout, os.Stderr, appsbom.GenerateInput{
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
