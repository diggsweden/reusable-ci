// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
)

func baseImagesCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCleanup,
		Usage: "delete promoted and stale staging base-image tags safely",
		Description: `Deletes staging container package versions through the Forgejo
package API, never by manifest digest. Final tags are resolved before and after
each promoted candidate deletion; stale staging versions are swept only after
their version names pass the base-image staging policy.`,
		Flags: append(baseImagesRepositoryFlags(),
			&cli.StringFlag{Name: "shared-core-images-json", Value: "[]", Sources: cli.EnvVars("SHARED_CORE_IMAGES_JSON"), Usage: "JSON array of shared-core base image metadata"},
			&cli.StringFlag{Name: "base-images-json", Value: "[]", Sources: cli.EnvVars("BASE_IMAGES_JSON"), Usage: "JSON array of base image metadata"},
			&cli.StringFlag{Name: flagBaseInputsJSON, Value: "[]", Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "JSON array mapping flavors to content/base input IDs"},
			regflags.AuthFile(regflags.AuthFileOpts{Usage: "registry auth file for final/staging digest checks"}),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := baseImagesCommonFromCmd(cmd, false, false)
			if err != nil {
				return err
			}

			sharedImages, err := parseBaseImages(cmd.String("shared-core-images-json"))
			if err != nil {
				return err
			}

			baseImages, err := parseBaseImages(cmd.String("base-images-json"))
			if err != nil {
				return err
			}

			baseInputs, err := parseBaseInputs(cmd.String(flagBaseInputsJSON))
			if err != nil {
				return err
			}

			registry := ociregistry.New()
			if authFile := cmd.String(flagAuthFile); authFile != "" {
				registry = ociregistry.WithAuthFile(authFile)
			}

			// Base-image cleanup is forge-gated (it drives the package API's
			// TagDeleter + ContainerPackageLister roles, and Forgejo is the
			// only forge implementing both) and
			// needs the normalized --server-url injected as the Forgejo server,
			// so it constructs the provider directly rather than resolving the
			// env-detected one through deps.
			forgeProvider := &forgejo.Provider{Env: func(key string) string {
				if key == "FORGEJO_SERVER_URL" && common.ServerURL != "" {
					return common.ServerURL
				}

				return os.Getenv(key)
			}}

			return appbaseimages.CleanupStagingBaseImages(ctx, registry, forgeProvider, os.Stderr, appbaseimages.BaseImageCleanupStagingInput{
				Images:             append(sharedImages, baseImages...),
				BaseInputs:         baseInputs,
				ExpectedRepository: common.ExpectedRepository,
			})
		},
	}
}
