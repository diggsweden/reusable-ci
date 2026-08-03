// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func baseImagesVerifyCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdValidate,
		Usage: "validate existing immutable final base-image tags and report missing flavors",
		Flags: append(baseImagesCommonFlags(),
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "optional single sha256 base input ID expected for every flavor"},
			&cli.StringFlag{Name: flagBaseInputsJSON, Value: "[]", Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "JSON array mapping flavors to sha256 base input IDs"},
			&cli.StringFlag{Name: "flavors-file", Value: "packaging/container/flavors.list", Sources: cli.EnvVars("FLAVORS_FILE"), Usage: "newline-delimited flavor list in the consumer checkout"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := baseImagesCommonFromCmd(cmd, false, true)
			if err != nil {
				return err
			}

			if keyErr := validateBaseImagesPublicKey(common.PublicKeyPath, common.PublicKeySHA256); keyErr != nil {
				return keyErr
			}

			flavors, err := readBaseImagesFlavors(cmd.String("flavors-file"))
			if err != nil {
				return err
			}

			baseInputs, err := parseBaseInputs(cmd.String(flagBaseInputsJSON))
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return withBaseImagesDockerConfig(func(authFile string) error {
					result, err := appbaseimages.VerifyExistingBaseImages(ctx,
						ociregistry.WithAuthFile(authFile),
						cosign.NewIsolated("DOCKER_CONFIG"),
						os.Stderr,
						appbaseimages.BaseImageVerifyExistingInput{
							Flavors:            flavors,
							BaseInputs:         baseInputs,
							BaseInputID:        cmd.String(flagBaseInputID),
							ExpectedRepository: common.ExpectedRepository,
							ExpectedSource:     common.ExpectedSource,
							ExpectedWorkflow:   common.ExpectedWorkflow,
							CosignPublicKey:    common.PublicKeyPath,
						})
					if err != nil {
						return err
					}

					return writeBaseImagesVerifyOutputs(ctx, dep, result)
				})
			})
		},
	}
}

func readBaseImagesFlavors(path string) ([]string, error) {
	if unsafeWorkflowPath(path) {
		return nil, fmt.Errorf("base images: unsafe flavors-file path: %s: %w", path, errs.ErrUsage)
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected path validated as relative.
	if err != nil {
		return nil, fmt.Errorf("base images: flavors file not found: %s: %w", path, errs.ErrMissingInput)
	}

	flavors := make([]string, 0)

	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		flavors = append(flavors, line)
	}

	if len(flavors) == 0 {
		return nil, fmt.Errorf("base images: flavors file contains no flavors: %s: %w", path, errs.ErrValidation)
	}

	return flavors, nil
}

func writeBaseImagesVerifyOutputs(ctx context.Context, dep *deps.Deps, result appbaseimages.BaseImageVerifyExistingResult) error {
	if err := dep.OutputSink.SetBool(ctx, "all_found", result.AllFound); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "all_images_json", result.AllImagesJSON); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "missing_flavors_json", result.MissingFlavorsJSON)
}
