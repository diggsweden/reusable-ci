// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
)

func baseImagesPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdPromote,
		Usage: "verify base-image evidence and promote candidate refs to immutable final tags",
		Flags: append(baseImagesCommonFlags(),
			&cli.StringFlag{Name: "all-images-json", Sources: cli.EnvVars("ALL_IMAGES_JSON"), Usage: "JSON array of base image metadata to verify/promote"},
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "optional single sha256 base input ID expected for every image"},
			regflags.Username(),
			regflags.PasswordFile(),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := baseImagesCommonFromCmd(cmd, true, true)
			if err != nil {
				return err
			}

			if keyErr := validateBaseImagesPublicKey(common.PublicKeyPath, common.PublicKeySHA256); keyErr != nil {
				return keyErr
			}

			images, err := parseBaseImages(cmd.String("all-images-json"))
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return withBaseImagesDockerConfig(func(authFile string) error {
					if err := common.login(authFile); err != nil {
						return err
					}

					result, err := appbaseimages.PromoteBaseImages(ctx,
						ociregistry.WithAuthFile(authFile),
						cosign.NewIsolated("DOCKER_CONFIG"),
						os.Stderr,
						appbaseimages.BaseImagePromoteInput{
							Images:             images,
							BaseInputID:        cmd.String(flagBaseInputID),
							ExpectedRepository: common.ExpectedRepository,
							ExpectedSource:     common.ExpectedSource,
							ExpectedWorkflow:   common.ExpectedWorkflow,
							CosignPublicKey:    common.PublicKeyPath,
						})
					if err != nil {
						return err
					}

					return writeBaseImagesPromoteOutputs(ctx, dep, result)
				})
			})
		},
	}
}

func writeBaseImagesPromoteOutputs(ctx context.Context, dep *deps.Deps, result appbaseimages.BaseImagePromoteResult) error {
	if err := dep.OutputSink.Set(ctx, "base_input_id", result.BaseInputID); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "base_images_json", result.BaseImagesJSON); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "base_input_ids_json", result.BaseInputIDsJSON)
}
