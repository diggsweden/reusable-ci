// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package security wires `reusable-ci security <subcmd>` to the
// security app/domain layers.
package security

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// New returns the `security` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "security",
		Usage: "security report transforms (Trivy → GitLab schemas)",
		Commands: []*cli.Command{
			trivyToGitLabDepCmd(),
			trivyToGitLabContainerCmd(),
			uploadSARIFCmd(),
			enrichGitHubSARIFCmd(),
			runOpengrepCmd(),
			scanDependenciesCmd(),
		},
	}
}

func trivyToGitLabDepCmd() *cli.Command {
	return &cli.Command{
		Name:      "trivy-to-gitlab-dep",
		Usage:     "convert Trivy JSON to a GitLab dependency-scanning report",
		ArgsUsage: "<trivy.json> <output.json>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "trivy-version",
				Sources: cli.EnvVars("TRIVY_VERSION"),
				Usage:   "version string to embed in scan.scanner.version (default: unknown)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args()
			if args.Len() < 2 {
				return fmt.Errorf("Usage: security trivy-to-gitlab-dep <trivy.json> <output.json>: %w", errs.ErrUsage)
			}
			n, err := appsecurity.TrivyToGitLabDep(appsecurity.TransformInput{
				InputPath:    args.Get(0),
				OutputPath:   args.Get(1),
				TrivyVersion: cmd.String("trivy-version"),
			})
			if err != nil {
				return err
			}
			fmt.Printf("Wrote GitLab dependency-scanning report: %s (%d findings)\n", args.Get(1), n)
			return nil
		},
	}
}

func trivyToGitLabContainerCmd() *cli.Command {
	return &cli.Command{
		Name:      "trivy-to-gitlab-container",
		Usage:     "convert Trivy JSON to a GitLab container-scanning report",
		ArgsUsage: "<trivy.json> <output.json>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "image-ref",
				Sources: cli.EnvVars("IMAGE_REF"),
				Usage:   "fully qualified image (registry/owner/name@sha256:…); falls back to ArtifactName from the Trivy JSON when empty",
			},
			&cli.StringFlag{
				Name:    "trivy-version",
				Sources: cli.EnvVars("TRIVY_VERSION"),
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args()
			if args.Len() < 2 {
				return fmt.Errorf("Usage: security trivy-to-gitlab-container <trivy.json> <output.json>: %w", errs.ErrUsage)
			}
			n, err := appsecurity.TrivyToGitLabContainer(appsecurity.TransformInput{
				InputPath:    args.Get(0),
				OutputPath:   args.Get(1),
				TrivyVersion: cmd.String("trivy-version"),
				ImageRef:     cmd.String("image-ref"),
			})
			if err != nil {
				return err
			}
			fmt.Printf("Wrote GitLab container-scanning report: %s (%d findings)\n", args.Get(1), n)
			return nil
		},
	}
}
