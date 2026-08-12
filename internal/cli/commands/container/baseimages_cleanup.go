// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func baseImagesCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCleanup,
		Usage: "delete promoted and stale staging base-image tags safely",
		Description: `Deletes staging container versions through the active forge's
package/registry API, never by manifest digest — staging and final tags share one
manifest, so a digest delete would destroy the promoted image. Final tags are
resolved before and after each promoted candidate deletion; stale staging versions
are swept only after their version names pass the base-image staging policy.

Requires a forge implementing both container tag listing and tag deletion
(see the capability matrix in docs/providers.md); others refuse with an
"unsupported" error rather than deleting unsafely.`,
		Flags: append(baseImagesRepositoryFlags(),
			&cli.StringFlag{Name: "shared-core-images-json", Value: "[]", Sources: cli.EnvVars("SHARED_CORE_IMAGES_JSON"), Usage: "JSON array of shared-core base image metadata"},
			&cli.StringFlag{Name: "base-images-json", Value: "[]", Sources: cli.EnvVars("BASE_IMAGES_JSON"), Usage: "JSON array of base image metadata"},
			&cli.StringFlag{Name: flagBaseInputsJSON, Value: "[]", Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "JSON array mapping flavors to content/base input IDs"},
			localRegistryFlag(),
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

			// TagDeleter + ContainerPackageLister, from whichever surface holds
			// the bases. Refusing by role means any forge implementing both is
			// supported and the rest get a typed "unsupported" refusal; the
			// local-registry path gets the same two methods from crane.
			cleaner, err := baseImagePackageRegistry(cmd, common, "base-image staging cleanup")
			if err != nil {
				return err
			}

			return appbaseimages.CleanupStagingBaseImages(ctx, registry, cleaner, os.Stderr, appbaseimages.BaseImageCleanupStagingInput{
				Images:             append(sharedImages, baseImages...),
				BaseInputs:         baseInputs,
				ExpectedRepository: common.ExpectedRepository,
			})
		},
	}
}

// baseImagePackageAPI is the package-API surface base-image staging cleanup and
// retention need. It mirrors the app's own composition of the two port roles so
// the CLI can refuse before doing any work, with a message naming the whole
// capability rather than whichever half was missing.
type baseImagePackageAPI interface {
	provider.TagDeleter
	provider.ContainerPackageLister
}
