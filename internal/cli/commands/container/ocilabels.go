// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

func releaseLabelsCmd() *cli.Command {
	return &cli.Command{
		Name:  "release-labels",
		Usage: "emit standard OCI release labels as Buildah --label argv tokens",
		Description: `Prints one argv token per line: alternating --label and key=value.
Shell callers can mapfile/readarray the output and pass the resulting array to
buildah bud without reimplementing label policy in shell.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Required: true, Sources: cli.EnvVars("OCI_LABEL_TITLE"), Usage: "org.opencontainers.image.title"},
			&cli.StringFlag{Name: flagVersion, Required: true, Sources: cli.EnvVars("OCI_LABEL_VERSION"), Usage: "org.opencontainers.image.version"},
			&cli.StringFlag{Name: "created", Required: true, Sources: cli.EnvVars("OCI_LABEL_CREATED"), Usage: "org.opencontainers.image.created (RFC 3339)"},
			&cli.StringFlag{Name: flagRevision, Required: true, Sources: cli.EnvVars("OCI_LABEL_REVISION"), Usage: "org.opencontainers.image.revision"},
			&cli.StringFlag{Name: flagRefName, Required: true, Sources: cli.EnvVars("OCI_LABEL_REF_NAME"), Usage: "org.opencontainers.image.ref.name"},
			&cli.StringFlag{Name: flagSource, Required: true, Sources: cli.EnvVars("OCI_LABEL_SOURCE"), Usage: "org.opencontainers.image.source and url"},
			&cli.StringFlag{Name: "documentation", Sources: cli.EnvVars("OCI_LABEL_DOCUMENTATION"), Usage: "org.opencontainers.image.documentation; defaults to <source>#readme"},
			&cli.StringFlag{Name: "description", Sources: cli.EnvVars("OCI_LABEL_DESCRIPTION"), Usage: "org.opencontainers.image.description"},
			&cli.StringFlag{Name: "licenses", Sources: cli.EnvVars("OCI_LABEL_LICENSES"), Usage: "org.opencontainers.image.licenses"},
			&cli.StringFlag{Name: "vendor", Sources: cli.EnvVars("OCI_LABEL_VENDOR"), Usage: "org.opencontainers.image.vendor"},
			&cli.StringFlag{Name: "authors", Sources: cli.EnvVars("OCI_LABEL_AUTHORS"), Usage: "org.opencontainers.image.authors"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			flags, err := appcontainer.OCIReleaseLabelFlags(releaseLabelsInput(cmd))
			if err != nil {
				return err
			}

			for _, flag := range flags {
				_, _ = fmt.Fprintln(os.Stdout, flag)
			}

			return nil
		},
	}
}

func releaseLabelsInput(cmd *cli.Command) appcontainer.OCIReleaseLabelsInput {
	return appcontainer.OCIReleaseLabelsInput{
		OCILabels: domaincontainer.OCILabels{
			Title:         cmd.String("title"),
			Version:       cmd.String(flagVersion),
			Revision:      cmd.String(flagRevision),
			RefName:       cmd.String(flagRefName),
			Documentation: cmd.String("documentation"),
			Description:   cmd.String("description"),
			Licenses:      cmd.String("licenses"),
			Vendor:        cmd.String("vendor"),
			Authors:       cmd.String("authors"),
		},
		Created: cmd.String("created"),
		Source:  cmd.String(flagSource),
	}
}

func releaseIdentityMatchesCmd() *cli.Command {
	return &cli.Command{
		Name:  "release-identity-matches",
		Usage: "print whether OCI labels identify the expected release",
		Description: `Reads a compact JSON object of image labels and prints true or
false. The source label comparison is case-insensitive to tolerate forge owner
case drift; revision, version, and ref.name are exact matches.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "labels-json", Required: true, Usage: "compact JSON object containing image labels"},
			&cli.StringFlag{Name: flagRevision, Required: true, Usage: "expected org.opencontainers.image.revision"},
			&cli.StringFlag{Name: flagVersion, Required: true, Usage: "expected org.opencontainers.image.version"},
			&cli.StringFlag{Name: flagRefName, Required: true, Usage: "expected org.opencontainers.image.ref.name"},
			&cli.StringFlag{Name: flagSource, Required: true, Usage: "expected org.opencontainers.image.source"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			matched, err := appcontainer.OCIReleaseIdentityMatches(cmd.String("labels-json"), appcontainer.OCIReleaseIdentityInput{
				Revision: cmd.String(flagRevision),
				Version:  cmd.String(flagVersion),
				RefName:  cmd.String(flagRefName),
				Source:   cmd.String(flagSource),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, matched)

			return nil
		},
	}
}

func imageLabelsJSONCmd() *cli.Command {
	return &cli.Command{
		Name:  "image-labels-json",
		Usage: "fetch an image's OCI config labels as compact JSON",
		Description: `Fetches the image config selected by --ref and prints its labels
as a compact JSON object. --auth-file accepts the Docker/containers auth config
written by ` + "`container login`" + `.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "image tag or digest ref to inspect"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "Docker-compatible registry auth config"}),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			registry := ociregistry.New()
			if authFile := cmd.String(flagAuthFile); authFile != "" {
				registry = ociregistry.WithAuthFile(authFile)
			}

			labels, err := appcontainer.OCIImageLabelsJSON(ctx, registry, cmd.String(flagRef))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, labels)

			return nil
		},
	}
}
