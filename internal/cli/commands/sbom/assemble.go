// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// assembleCmd wires `reusable-ci sbom assemble` — assemble a project's CISA
// SBOM layer set. It HARVESTS the language-native build BOM (e.g. the
// cyclonedx-gomod `bom.json` your build step emits) and SYFT-SCANS built
// artifacts/containers, then normalises everything into the canonical
// per-layer filenames (optionally bundling them into a release zip).
//
// It runs AFTER the build: the build layer copies a pre-produced BOM rather
// than generating one, because the language's own dependency tooling resolves
// a more accurate graph than a filesystem scan. The scan layers need a built
// artifact (`analyzed-artifact`) or an image (`analyzed-container`).
func assembleCmd() *cli.Command {
	return &cli.Command{
		Name:  "assemble",
		Usage: "assemble a project's CISA SBOM layer set (harvest the build BOM + syft-scan artifacts/containers)",
		Description: `Assembles the requested CISA layers into canonical SBOM filenames. Runs after
the build: the 'build' layer harvests the language-native BOM your build emits
(cyclonedx-gomod / build-go.yml, cyclonedx-maven-plugin, …); the scan layers
syft-scan a built artifact or a container image.

EXAMPLES:
   # Assemble every layer for the auto-detected project (the default)
   reusable-ci sbom assemble

   # Just the build layer (harvested from the pre-produced bom.json) for npm
   reusable-ci sbom assemble --project-type=npm --layers=build

   # Build + analyzed-container for a Go service (--container-image is
   # required whenever the analyzed-container layer is requested)
   reusable-ci sbom assemble --project-type=go \
       --layers=build,analyzed-container \
       --container-image=ghcr.io/examplescope/myapp:v1.2.3

   # Bundle the assembled layers into a release-attached zip
   reusable-ci sbom assemble --project-type=maven --create-zip`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Value: string(projecttype.Auto), Usage: "ecosystem driving the syft scan (auto/maven/gradle/npm/go/cargo/…)"},
			&cli.StringFlag{Name: "layers", Value: "all", Usage: "comma/space/newline-separated CISA layers to assemble: all | build | analyzed-artifact | analyzed-container (\"all\" = every layer)"},
			&cli.StringFlag{Name: "version", Usage: "release version embedded in the SBOM filenames"},
			&cli.StringFlag{Name: "name", Usage: "project slug used as the SBOM filename prefix"},
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Usage: "directory syft scans"},
			&cli.StringFlag{Name: "container-image", Usage: "container image to analyze (required for the analyzed-container layer)"},
			&cli.BoolFlag{Name: "create-zip", Usage: "additionally bundle the layers into a release-attached zip"},
			// Plan mode (per-artifact matrix; was `sbom generate artifacts`).
			&cli.StringFlag{Name: "plan", Sources: cli.EnvVars("CONFIG_PLAN_JSON"), Usage: "typed config-plan JSON: assemble one layer set per artifact in the plan (from `config parse-artifacts`)"},
			&cli.StringFlag{Name: "sboms", Value: "all", Sources: cli.EnvVars("SBOMS"), Usage: "plan mode: sboms enum (\"all\", \"build,source\", …) selecting which layers to produce per artifact"},
			// Multi-artifact container mode (was `sbom generate container`).
			&cli.StringFlag{Name: "image-name", Sources: cli.EnvVars("IMAGE_NAME"), Usage: "container mode: base image name without tag/digest; with --image-digest forms the syft target"}, //nolint:goconst // generic flag identifier shared across container commands.
			&cli.StringFlag{Name: "image-digest", Sources: cli.EnvVars("IMAGE_DIGEST"), Usage: "container mode: sha256:… digest pinning the exact image manifest"},
			&cli.StringFlag{Name: "artifact-types", Sources: cli.EnvVars("ARTIFACT_TYPES"), Usage: "container mode: ecosystems embedded in the multi-artifact container SBOM"},
			&cli.StringFlag{Name: "ref-name", Sources: cienv.RefName(), Usage: "container mode: git ref name (leading \"v\" stripped) used in the SBOM filename"},
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "container mode: \"owner/repo\" used to derive the project slug"},
			// Signing (any mode): cosign-sign each assembled SBOM into a .bundle.
			&cli.BoolFlag{Name: "sign", Sources: cli.EnvVars("SIGN_SBOMS"), Usage: "cosign-sign each assembled SBOM (produces <file>.bundle); reuses the `release sign` signing path"},
			&cli.StringFlag{Name: "sign-method", Value: "sigstore", Sources: cli.EnvVars("SIGN_METHOD"), Usage: "signing method when --sign: \"sigstore\" (keyless OIDC) or \"kms\""},
			&cli.StringFlag{Name: "sign-key", Sources: cli.EnvVars("SIGN_KEY"), Usage: "cosign --key for --sign-method=kms (KMS/PKCS#11 URI or key-file path); forbidden for sigstore"},
			&cli.StringFlag{Name: "oidc-issuer", Sources: cli.EnvVars("SIGN_OIDC_ISSUER"), Usage: "OIDC issuer for --sign-method=sigstore; defaults to the detected forge's"},
		},
		// One verb: assemble (one of three modes), then optionally sign. The
		// signer is built FIRST so a bad --sign-method fails fast, before any
		// assembly work.
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var signer appsbom.FileSigner

			if cmd.Bool("sign") {
				built, err := buildSBOMSigner(cmd)
				if err != nil {
					return err
				}

				signer = built
			}

			if err := runAssembleMode(ctx, cmd); err != nil {
				return err
			}

			if signer == nil {
				return nil
			}

			return appsbom.SignAssembled(ctx, signer, cmd.String(flagWorkingDir), os.Stderr, os.Stderr)
		},
	}
}

// runAssembleMode dispatches the assemble mode by the flags/env present, in
// precedence order: --plan → per-artifact matrix; --image-name/-digest →
// multi-artifact container; otherwise → single-project assembly.
func runAssembleMode(ctx context.Context, cmd *cli.Command) error {
	switch {
	case cmd.String("plan") != "":
		return appsbom.GenerateArtifacts(ctx, syft.New(), maven.New(), git.New(), os.Stderr, os.Stderr, appsbom.GenerateArtifactsInput{
			ConfigPlanJSON: cmd.String("plan"),
			SBOMs:          cmd.String("sboms"),
			Version:        cmd.String("version"),
			WorkingDir:     cmd.String(flagWorkingDir),
		})
	case cmd.String("image-name") != "" || cmd.String("image-digest") != "":
		return appsbom.GenerateContainer(ctx, syft.New(), maven.New(), git.New(), os.Stderr, os.Stderr, appsbom.GenerateContainerInput{
			ArtifactTypes: cmd.String("artifact-types"),
			RefName:       cmd.String("ref-name"),
			Repo:          cmd.String("repository"),
			ImageName:     cmd.String("image-name"),
			ImageDigest:   cmd.String("image-digest"),
		})
	default:
		return appsbom.Generate(ctx, syft.New(), maven.New(), git.New(), buildSBOMGenerator{}, os.Stderr, os.Stderr, appsbom.GenerateInput{
			ProjectType:    cmd.String("project-type"),
			Layers:         cmd.String("layers"),
			Version:        cmd.String("version"),
			Name:           cmd.String("name"),
			WorkingDir:     cmd.String(flagWorkingDir),
			ContainerImage: cmd.String("container-image"),
			CreateZip:      cmd.Bool("create-zip"),
		})
	}
}

// buildSBOMSigner constructs the cosign signer used by --sign, reusing the same
// CosignSigner path as `release sign`. Sigstore (keyless) resolves the detected
// forge's OIDC issuer when none is given; KMS takes an explicit --sign-key.
func buildSBOMSigner(cmd *cli.Command) (appsbom.FileSigner, error) {
	var method domainrelease.SignMethod

	switch methodName := cmd.String("sign-method"); methodName {
	case "sigstore", "":
		method = domainrelease.SignMethodSigstore
	case "kms":
		method = domainrelease.SignMethodKMS
	default:
		return nil, fmt.Errorf("--sign-method must be \"sigstore\" or \"kms\", got %q: %w", methodName, errs.ErrUsage)
	}

	oidcIssuer := cmd.String("oidc-issuer")
	if method == domainrelease.SignMethodSigstore && oidcIssuer == "" {
		oidcIssuer = apprelease.DefaultOIDCIssuer(deps.DescriberForDetected())
	}

	endpoints := signflags.ReadEndpoints(cmd)

	return apprelease.NewCosignSigner(cosign.New(), apprelease.CosignSignerInput{
		Method:     method,
		KeyRef:     cmd.String("sign-key"),
		OIDCIssuer: oidcIssuer,
		FulcioURL:  endpoints.FulcioURL,
		RekorURL:   endpoints.RekorURL,
	}, os.Stderr)
}
