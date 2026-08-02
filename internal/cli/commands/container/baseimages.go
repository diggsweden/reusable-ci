// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/skopeo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var (
	baseImagesHex64RE            = regexp.MustCompile(`^[0-9a-f]{64}$`)
	baseImagesRepositorySuffixRE = regexp.MustCompile(`^(-[A-Za-z0-9][A-Za-z0-9._-]*)?$`)
)

type baseImagesCommon struct {
	ServerURL          string
	ServerHost         string
	Registry           string
	Repository         string
	RepositorySuffix   string
	ExpectedRepository string
	ExpectedSource     string
	RegistryUsername   string
	RegistryPassword   string
	PublicKeyPath      string
	PublicKeySHA256    string
	ExpectedWorkflow   string
}

func baseImagesGroup() *cli.Command {
	return &cli.Command{
		Name:  "base-images",
		Usage: "sign, verify, and promote forgejo-ci base-image caches",
		Description: `Workflow-facing base-image boundary. The commands validate
flavor/base-input metadata, verify Cosign signatures plus CycloneDX and SLSA
lineage attestations, sign digest-pinned base images, and promote verified
candidate images to immutable final base tags. Checkout and pinned-binary
bootstrap remain owned by the caller.`,
		Commands: []*cli.Command{
			baseImagesCollectCmd(),
			baseImagesInputFieldCmd(),
			baseImagesArchRefCmd(),
			baseImagesArchMetadataCmd(),
			baseImagesCandidateMetadataCmd(),
			baseImagesSignCmd(),
			baseImagesVerifyExistingCmd(),
			baseImagesPromoteCmd(),
			baseImagesCleanupStagingCmd(),
		},
	}
}

func baseImagesCollectCmd() *cli.Command {
	return &cli.Command{
		Name:  "collect",
		Usage: "collect verified and built base-image metadata for signing and promotion",
		Description: `Fan-in deterministic base-image metadata after verification/build
jobs have produced their artifacts. The caller still owns artifact download,
SBOM staging, and job orchestration; reusable-ci owns JSON merging, sorting,
signing projections, unresolved-flavor calculation, and optional decision JSON.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "already-verified", Sources: cli.EnvVars("BASES_ALREADY_VERIFIED", "BASE_IMAGES_ALREADY_VERIFIED"), Usage: "verified images already satisfy the request; ignore built metadata"},
			&cli.StringFlag{Name: "verified-images-json", Value: "[]", Sources: cli.EnvVars("VERIFIED_BASE_IMAGES_JSON", "VERIFIED_SHARED_CORE_JSON"), Usage: "JSON array of already verified base images"},
			&cli.StringFlag{Name: "verified-file", Sources: cli.EnvVars("VERIFIED_BASE_IMAGES_FILE"), Usage: "optional JSON file containing verified images"},
			&cli.StringSliceFlag{Name: "built-file", Sources: cli.EnvVars("BUILT_BASE_IMAGE_FILE"), Usage: "built base image metadata file (object or array); missing files are ignored"},
			&cli.StringFlag{Name: "built-dir", Sources: cli.EnvVars("BUILT_BASE_IMAGE_DIR"), Usage: "directory containing built base image metadata files"},
			&cli.StringFlag{Name: "built-pattern", Value: "base-image-*.json", Sources: cli.EnvVars("BUILT_BASE_IMAGE_PATTERN"), Usage: "glob pattern under --built-dir for built metadata"},
			&cli.StringFlag{Name: "missing-flavors-json", Value: "[]", Sources: cli.EnvVars("MISSING_FLAVORS_JSON"), Usage: "JSON array of flavors missing before build"},
			&cli.BoolFlag{Name: "include-signing-sbom-sha256", Sources: cli.EnvVars("BASE_IMAGES_SIGNING_INCLUDE_SBOM_SHA256"), Usage: "include sbom_sha256 in signing images, using an empty string when absent"},
			&cli.StringFlag{Name: "base-input-set-id", Sources: cli.EnvVars("BASE_INPUT_SET_ID", "BASE_INPUT_ID"), Usage: "base input set sha256 for decision JSON"},
			&cli.StringFlag{Name: flagBaseInputsJSON, Value: "[]", Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "base input JSON array for decision JSON"},
			&cli.StringFlag{Name: flagSourceSHA, Sources: cli.EnvVars("SOURCE_SHA"), Usage: "source commit sha for decision JSON"},
			&cli.StringFlag{Name: "decision-output", Sources: cli.EnvVars("BASE_DECISION_OUTPUT"), Usage: "optional path to write compact base decision JSON"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			input, err := baseImagesCollectInputFromCmd(cmd)
			if err != nil {
				return err
			}

			result, err := appcontainer.CollectBaseImages(input)
			if err != nil {
				return err
			}

			if output := cmd.String("decision-output"); output != "" {
				if err := writeBaseImagesMetadataFile(output, []byte(result.DecisionJSON+"\n")); err != nil {
					return err
				}
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				if err := dep.OutputSink.Set(ctx, "images-json", result.ImagesJSON); err != nil {
					return err
				}

				if err := dep.OutputSink.Set(ctx, "all-images-json", result.AllImagesJSON); err != nil {
					return err
				}

				return dep.OutputSink.Set(ctx, "unresolved-flavors-json", result.UnresolvedFlavorsJSON)
			})
		},
	}
}

// baseImagesCollectInputFromCmd reads and parses every collect input flag
// into the app-layer input, keeping the collect Action itself flat.
func baseImagesCollectInputFromCmd(cmd *cli.Command) (appcontainer.BaseImageCollectInput, error) {
	verified, err := readBaseCollectVerifiedImages(cmd.String("verified-images-json"), cmd.String("verified-file"))
	if err != nil {
		return appcontainer.BaseImageCollectInput{}, err
	}

	builtFiles, err := baseCollectBuiltFiles(cmd.StringSlice("built-file"), cmd.String("built-dir"), cmd.String("built-pattern"))
	if err != nil {
		return appcontainer.BaseImageCollectInput{}, err
	}

	built, err := readBaseCollectImageFiles(builtFiles, true)
	if err != nil {
		return appcontainer.BaseImageCollectInput{}, err
	}

	missing, err := parseBaseCollectFlavors(cmd.String("missing-flavors-json"))
	if err != nil {
		return appcontainer.BaseImageCollectInput{}, err
	}

	baseInputs, err := parseBaseInputs(cmd.String(flagBaseInputsJSON))
	if err != nil {
		return appcontainer.BaseImageCollectInput{}, err
	}

	return appcontainer.BaseImageCollectInput{
		AlreadyVerified:          cmd.Bool("already-verified"),
		VerifiedImages:           verified,
		BuiltImages:              built,
		MissingFlavors:           missing,
		IncludeSigningSBOMSHA256: cmd.Bool("include-signing-sbom-sha256"),
		EmitDecision:             cmd.String("decision-output") != "",
		BaseInputSetID:           cmd.String("base-input-set-id"),
		BaseInputs:               baseInputs,
		SourceSHA:                cmd.String(flagSourceSHA),
	}, nil
}

func baseImagesInputFieldCmd() *cli.Command {
	return &cli.Command{
		Name:  "input-field",
		Usage: "print one field from base-inputs JSON for a flavor",
		Description: `Reads the base-inputs JSON produced by base-graph planning and
prints one selected field for a flavor. This replaces shell jq lookups while
keeping caller-owned loop and tag orchestration in shell.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagBaseInputsJSON, Required: true, Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "JSON array mapping flavors to content/base input IDs"},
			&cli.StringFlag{Name: flagFlavor, Required: true, Sources: cli.EnvVars("BASE_FLAVOR", "FLAVOR"), Usage: "base flavor to select"},
			&cli.StringFlag{Name: "field", Value: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_FIELD"), Usage: "field to print: base-input-id or content-id"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			inputs, err := parseBaseInputs(cmd.String(flagBaseInputsJSON))
			if err != nil {
				return err
			}

			value, err := appcontainer.BaseInputField(appcontainer.BaseInputFieldInput{
				Inputs: inputs,
				Flavor: cmd.String(flagFlavor),
				Field:  cmd.String("field"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, value)

			return nil
		},
	}
}

func baseImagesArchRefCmd() *cli.Command {
	return &cli.Command{
		Name:  flagArchRef,
		Usage: "print a verified per-architecture base-image ref from metadata JSON",
		Description: `Reads one base arch metadata JSON file, checks the expected
flavor, architecture, and content ID, then prints the digest-pinned arch ref.
The caller still owns artifact paths and Buildah manifest assembly.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFile, Required: true, Sources: cli.EnvVars("BASE_ARCH_METADATA_FILE"), Usage: "base arch metadata JSON file"},
			&cli.StringFlag{Name: flagFlavor, Sources: cli.EnvVars("BASE_FLAVOR", "FLAVOR"), Usage: "optional expected base flavor"},
			&cli.StringFlag{Name: flagArch, Required: true, Sources: cli.EnvVars("BASE_ARCH"), Usage: "expected architecture, e.g. amd64 or arm64"},
			&cli.StringFlag{Name: flagContentID, Required: true, Sources: cli.EnvVars("BASE_CONTENT_ID"), Usage: "expected sha256 content ID"},
			&cli.StringFlag{Name: flagRepository, Sources: cli.EnvVars("BASE_REPO", "EXPECTED_REPOSITORY"), Usage: "optional expected base image repository for arch_ref"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			metadata, err := readBaseArchMetadata(cmd.String(flagFile))
			if err != nil {
				return err
			}

			ref, err := appcontainer.BaseArchRef(appcontainer.BaseArchRefInput{
				Metadata:   metadata,
				Flavor:     cmd.String(flagFlavor),
				Arch:       cmd.String(flagArch),
				ContentID:  cmd.String(flagContentID),
				Repository: cmd.String(flagRepository),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, ref)

			return nil
		},
	}
}

func baseImagesCandidateMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "candidate-metadata",
		Usage: "write assembled candidate base-image metadata JSON for signing and promotion",
		Description: `Formats the deterministic assembled base-image metadata consumed
by signing and promotion. The caller still owns manifest construction, scan/SBOM
policy, and output path; reusable-ci owns validation and the JSON shape.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFlavor, Required: true, Sources: cli.EnvVars("BASE_FLAVOR", "FLAVOR"), Usage: "base flavor name"},
			&cli.StringFlag{Name: flagRepository, Sources: cli.EnvVars("BASE_REPO", "EXPECTED_REPOSITORY"), Usage: "base image repository used to validate tags and refs"},
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cli.EnvVars("FINAL_TAG", "BASE_TAG"), Usage: "final immutable base image tag"},
			&cli.StringFlag{Name: flagRef, Required: true, Sources: cli.EnvVars("IMAGE_REF", "BASE_REF"), Usage: "digest-pinned image ref for the candidate digest"},
			&cli.StringFlag{Name: "candidate-tag", Required: true, Sources: cli.EnvVars("CANDIDATE_TAG", "BASE_CANDIDATE_TAG"), Usage: "candidate staging tag"},
			&cli.StringFlag{Name: "candidate-ref", Sources: cli.EnvVars("CANDIDATE_REF", "BASE_CANDIDATE_REF"), Usage: "candidate digest-pinned ref; defaults to --ref"},
			&cli.StringFlag{Name: flagBaseInputID, Required: true, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "sha256 base input ID"},
			&cli.StringFlag{Name: flagContentID, Required: true, Sources: cli.EnvVars("BASE_CONTENT_ID"), Usage: "sha256 content ID"},
			&cli.StringFlag{Name: "sbom-sha256", Sources: cli.EnvVars("SBOM_SHA256", "BASE_SBOM_SHA256"), Usage: "optional sha256 of the delivered base SBOM"},
			&cli.StringFlag{Name: "output", Value: "-", Sources: cli.EnvVars("BASE_CANDIDATE_METADATA_OUTPUT"), Usage: "output JSON path; '-' writes stdout"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			body, err := appcontainer.BaseCandidateMetadataJSON(appcontainer.BaseCandidateMetadataInput{
				Flavor:       cmd.String(flagFlavor),
				Repository:   cmd.String(flagRepository),
				Tag:          cmd.String(flagTag),
				Ref:          cmd.String(flagRef),
				CandidateTag: cmd.String("candidate-tag"),
				CandidateRef: cmd.String("candidate-ref"),
				BaseInputID:  cmd.String(flagBaseInputID),
				ContentID:    cmd.String(flagContentID),
				SBOMSHA256:   cmd.String("sbom-sha256"),
			})
			if err != nil {
				return err
			}

			return writeBaseImagesMetadataFile(cmd.String("output"), []byte(body))
		},
	}
}

func baseImagesArchMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  "arch-metadata",
		Usage: "write per-architecture base-image metadata JSON for manifest assembly",
		Description: `Formats the deterministic per-architecture base metadata consumed
by base-image manifest assembly. The caller still owns the build, reuse decision,
and output path; reusable-ci owns validation and the JSON shape.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFlavor, Required: true, Sources: cli.EnvVars("BASE_FLAVOR", "FLAVOR"), Usage: "base flavor name"},
			&cli.StringFlag{Name: flagArch, Required: true, Sources: cli.EnvVars("BASE_ARCH"), Usage: "base architecture, e.g. amd64 or arm64"},
			&cli.StringFlag{Name: flagRepository, Sources: cli.EnvVars("BASE_REPO", "EXPECTED_REPOSITORY"), Usage: "base image repository used to derive --arch-ref from --arch-digest"},
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cli.EnvVars("FINAL_TAG", "BASE_TAG"), Usage: "final immutable base image tag"},
			&cli.StringFlag{Name: "arch-tag", Required: true, Sources: cli.EnvVars("ARCH_TAG", "BASE_ARCH_TAG"), Usage: "per-architecture staging tag"},
			&cli.StringFlag{Name: "arch-digest", Sources: cli.EnvVars("ARCH_DIGEST", "BASE_ARCH_DIGEST"), Usage: "per-architecture digest, sha256:<hex>; used with --repository when --arch-ref is empty"},
			&cli.StringFlag{Name: flagArchRef, Sources: cli.EnvVars("ARCH_REF", "BASE_ARCH_REF"), Usage: "optional digest-pinned per-architecture ref"},
			&cli.StringFlag{Name: flagBaseInputID, Required: true, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "sha256 base input ID"},
			&cli.StringFlag{Name: flagContentID, Required: true, Sources: cli.EnvVars("BASE_CONTENT_ID"), Usage: "sha256 content ID"},
			&cli.StringFlag{Name: "output", Value: "-", Sources: cli.EnvVars("BASE_ARCH_METADATA_OUTPUT"), Usage: "output JSON path; '-' writes stdout"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			body, err := appcontainer.BaseArchMetadataJSON(appcontainer.BaseArchMetadataInput{
				Flavor:      cmd.String(flagFlavor),
				Arch:        cmd.String(flagArch),
				Repository:  cmd.String(flagRepository),
				Tag:         cmd.String(flagTag),
				ArchTag:     cmd.String("arch-tag"),
				ArchDigest:  cmd.String("arch-digest"),
				ArchRef:     cmd.String(flagArchRef),
				BaseInputID: cmd.String(flagBaseInputID),
				ContentID:   cmd.String(flagContentID),
			})
			if err != nil {
				return err
			}

			return writeBaseImagesMetadataFile(cmd.String("output"), []byte(body))
		},
	}
}

func writeBaseImagesMetadataFile(output string, body []byte) error {
	if output != cliio.StdSentinel {
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil { //nolint:gosec,mnd // public metadata artifact path selected by caller.
			return fmt.Errorf("base images metadata: create output directory: %w", err)
		}
	}

	return cliio.WriteFile(output, body, 0o644) //nolint:gosec // base metadata is public release evidence.
}

func baseImagesSignCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdSign,
		Usage: "sign and attest digest-pinned base images with SBOM and SLSA lineage evidence",
		Flags: append(baseImagesRepositoryFlags(),
			&cli.StringFlag{Name: "images-json", Sources: cli.EnvVars("IMAGES_JSON"), Usage: "JSON array of base image metadata to sign"},
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "optional single sha256 base input ID expected for every image"},
			&cli.StringFlag{Name: flagSourceSHA, Sources: cli.EnvVars("SOURCE_SHA"), Usage: "source commit SHA that produced the base images"},
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("CONTAINER_BUILD_TYPE", "BUILD_TYPE"), Usage: "SLSA buildType URI recorded in base lineage"},
			&cli.StringFlag{Name: "key", Value: "env://COSIGN_KEY", Sources: cli.EnvVars("COSIGN_KEY_REF"), Usage: "cosign --key reference used for signing; default reads the signing key from $COSIGN_KEY"},
			&cli.StringFlag{Name: "premade-sbom-dir", Sources: cli.EnvVars("PREMADE_SBOM_DIR"), Usage: "directory containing pre-built base-sbom-<flavor>.cyclonedx.json files; when set, every image must provide a matching sbom_sha256"},
			&cli.StringFlag{Name: flagRegistryUsername, Sources: cli.EnvVars(envRegistryUser, "REGISTRY_USERNAME"), Usage: usageRegistryUsername},
			&cli.StringFlag{Name: flagRegistryPasswordFile, Usage: usageRegistryPasswordFile},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := baseImagesCommonFromCmd(cmd, true, false)
			if err != nil {
				return err
			}

			images, err := parseBaseImages(cmd.String("images-json"))
			if err != nil {
				return err
			}

			return withBaseImagesDockerConfig(func(authFile string) error {
				if err := common.login(authFile); err != nil {
					return err
				}

				return appcontainer.SignBaseImages(ctx,
					cosign.NewIsolated("COSIGN_KEY", "COSIGN_PASSWORD", "DOCKER_CONFIG"),
					cosign.NewIsolated("DOCKER_CONFIG"),
					&syft.Adapter{UnsetEnv: signerSecretEnv()},
					&skopeo.Adapter{AuthFile: authFile, UnsetEnv: signerSecretEnv()},
					ociregistry.WithAuthFile(authFile),
					os.Stderr,
					os.Stderr,
					appcontainer.BaseImageSignInput{
						Images:             images,
						BaseInputID:        cmd.String(flagBaseInputID),
						ExpectedRepository: common.ExpectedRepository,
						ExpectedSource:     common.ExpectedSource,
						ExpectedWorkflow:   common.ExpectedWorkflow,
						SourceSHA:          cmd.String(flagSourceSHA),
						BuildType:          cmd.String("build-type"),
						KeyRef:             cmd.String("key"),
						PremadeSBOMDir:     cmd.String("premade-sbom-dir"),
					})
			})
		},
	}
}

func baseImagesVerifyExistingCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdVerifyExisting,
		Usage: "verify existing immutable final base-image tags and report missing flavors",
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
					result, err := appcontainer.VerifyExistingBaseImages(ctx,
						ociregistry.WithAuthFile(authFile),
						cosign.NewIsolated("DOCKER_CONFIG"),
						os.Stderr,
						appcontainer.BaseImageVerifyExistingInput{
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

func baseImagesPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdPromote,
		Usage: "verify base-image evidence and promote candidate refs to immutable final tags",
		Flags: append(baseImagesCommonFlags(),
			&cli.StringFlag{Name: "all-images-json", Sources: cli.EnvVars("ALL_IMAGES_JSON"), Usage: "JSON array of base image metadata to verify/promote"},
			&cli.StringFlag{Name: flagBaseInputID, Sources: cli.EnvVars("BASE_INPUT_ID"), Usage: "optional single sha256 base input ID expected for every image"},
			&cli.StringFlag{Name: flagRegistryUsername, Sources: cli.EnvVars(envRegistryUser, "REGISTRY_USERNAME"), Usage: usageRegistryUsername},
			&cli.StringFlag{Name: flagRegistryPasswordFile, Usage: usageRegistryPasswordFile},
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

					result, err := appcontainer.PromoteBaseImages(ctx,
						ociregistry.WithAuthFile(authFile),
						cosign.NewIsolated("DOCKER_CONFIG"),
						os.Stderr,
						appcontainer.BaseImagePromoteInput{
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

func baseImagesCleanupStagingCmd() *cli.Command {
	return &cli.Command{
		Name:  "cleanup-staging",
		Usage: "delete promoted and stale staging base-image tags safely",
		Description: `Deletes staging container package versions through the Forgejo
package API, never by manifest digest. Final tags are resolved before and after
each promoted candidate deletion; stale staging versions are swept only after
their version names pass the base-image staging policy.`,
		Flags: append(baseImagesRepositoryFlags(),
			&cli.StringFlag{Name: "shared-core-images-json", Value: "[]", Sources: cli.EnvVars("SHARED_CORE_IMAGES_JSON"), Usage: "JSON array of shared-core base image metadata"},
			&cli.StringFlag{Name: "base-images-json", Value: "[]", Sources: cli.EnvVars("BASE_IMAGES_JSON"), Usage: "JSON array of base image metadata"},
			&cli.StringFlag{Name: flagBaseInputsJSON, Value: "[]", Sources: cli.EnvVars("BASE_INPUTS_JSON"), Usage: "JSON array mapping flavors to content/base input IDs"},
			&cli.StringFlag{Name: flagAuthFile, Sources: cli.EnvVars("REUSABLE_CI_REGISTRY_AUTH_FILE"), Usage: "registry auth file for final/staging digest checks"},
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

			forgeProvider := &forgejo.Provider{Env: func(key string) string {
				if key == "FORGEJO_SERVER_URL" && common.ServerURL != "" {
					return common.ServerURL
				}

				return os.Getenv(key)
			}}

			return appcontainer.CleanupStagingBaseImages(ctx, registry, forgeProvider, os.Stderr, appcontainer.BaseImageCleanupStagingInput{
				Images:             append(sharedImages, baseImages...),
				BaseInputs:         baseInputs,
				ExpectedRepository: common.ExpectedRepository,
			})
		},
	}
}

func baseImagesCommonFlags() []cli.Flag {
	return append(baseImagesRepositoryFlags(), baseImagesPublicKeyFlags()...)
}

func baseImagesRepositoryFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: flagServerURL, Sources: cienv.ServerURL(), Usage: "forge server URL used to derive the registry host and expected source"},
		&cli.StringFlag{Name: flagRepository, Sources: cienv.Repository(), Usage: "owner/repo used to derive the expected source and base-image repository"},
		&cli.StringFlag{Name: "repository-suffix", Sources: cli.EnvVars("REPOSITORY_SUFFIX"), Usage: "optional suffix appended to the repository package name, e.g. -base"},
		&cli.StringFlag{Name: "expected-repository", Sources: cli.EnvVars("EXPECTED_REPOSITORY", "BASE_IMAGES_EXPECTED_REPOSITORY"), Usage: "exact base-image repository allowed for tags/refs (default: host/lower(owner/repo)<suffix>)"},
		&cli.StringFlag{Name: "expected-source", Sources: cli.EnvVars("EXPECTED_SOURCE", "BASE_IMAGES_EXPECTED_SOURCE"), Usage: "source repository URL expected in SLSA lineage (default: <server-url>/<repository>)"},
		&cli.StringFlag{Name: "caller-workflow", Sources: cli.EnvVars("EXPECTED_WORKFLOW", "CALLER_WORKFLOW"), Usage: "workflow filename expected in SLSA lineage"},
		&cli.StringFlag{Name: flagRegistry, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry host for promotion auth (default: host from --server-url)"},
	}
}

func baseImagesPublicKeyFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "cosign-public-key-path", Sources: cli.EnvVars("COSIGN_PUBLIC_KEY_PATH"), Usage: "relative path to the trusted Cosign public key in the consumer checkout"},
		&cli.StringFlag{Name: "cosign-public-key-sha256", Sources: cli.EnvVars("COSIGN_PUBLIC_KEY_SHA256"), Usage: "expected sha256 digest of the trusted Cosign public key"},
	}
}

func baseImagesCommonFromCmd(cmd *cli.Command, requireRegistry, requirePublicKey bool) (baseImagesCommon, error) {
	serverURL := normalizedServerURL(cmd.String(flagServerURL))

	serverHost, err := optionalRegistryHost(serverURL)
	if err != nil {
		return baseImagesCommon{}, err
	}

	registry, err := resolveRegistryHostFromFlags(cmd, serverHost)
	if err != nil {
		return baseImagesCommon{}, err
	}

	if requireRegistry && registry == "" {
		return baseImagesCommon{}, fmt.Errorf("base images: --registry or --server-url is required for registry login: %w", errs.ErrUsage)
	}

	repositorySuffix := strings.TrimSpace(cmd.String("repository-suffix"))
	if !baseImagesRepositorySuffixRE.MatchString(repositorySuffix) {
		return baseImagesCommon{}, fmt.Errorf("base images: repository-suffix must be empty or match -[A-Za-z0-9][A-Za-z0-9._-]*: %w", errs.ErrUsage)
	}

	repository := strings.TrimSpace(cmd.String(flagRepository))

	expectedRepository, err := baseImagesExpectedRepository(cmd, serverHost, repository, repositorySuffix)
	if err != nil {
		return baseImagesCommon{}, err
	}

	expectedSource, err := baseImagesExpectedSource(cmd, serverURL, repository)
	if err != nil {
		return baseImagesCommon{}, err
	}

	workflow := strings.TrimSpace(cmd.String("caller-workflow"))
	if unsafeWorkflowPath(workflow) {
		return baseImagesCommon{}, fmt.Errorf("base images: unsafe caller-workflow path: %s: %w", workflow, errs.ErrUsage)
	}

	publicKeyPath, publicKeySHA256, err := baseImagesPublicKeyFromCmd(cmd, requirePublicKey)
	if err != nil {
		return baseImagesCommon{}, err
	}

	registryUsername, registryPassword := baseImagesRegistryCreds(cmd, requireRegistry)

	return baseImagesCommon{
		ServerURL:          serverURL,
		ServerHost:         serverHost,
		Registry:           registry,
		Repository:         repository,
		RepositorySuffix:   repositorySuffix,
		ExpectedRepository: expectedRepository,
		ExpectedSource:     expectedSource,
		RegistryUsername:   registryUsername,
		RegistryPassword:   registryPassword,
		PublicKeyPath:      publicKeyPath,
		PublicKeySHA256:    publicKeySHA256,
		ExpectedWorkflow:   workflow,
	}, nil
}

// baseImagesRegistryCreds reads the registry credentials for verbs that
// log in; verbs without registry login leave both empty.
func baseImagesRegistryCreds(cmd *cli.Command, requireRegistry bool) (string, string) {
	if !requireRegistry {
		return "", ""
	}

	return strings.TrimSpace(cmd.String(flagRegistryUsername)), cmd.String(flagRegistryPasswordFile)
}

// baseImagesExpectedRepository resolves the exact base-image repository:
// the explicit --expected-repository flag, else host/lower(owner/repo)
// plus the validated repository suffix.
func baseImagesExpectedRepository(cmd *cli.Command, serverHost, repository, repositorySuffix string) (string, error) {
	expectedRepository := strings.TrimSpace(cmd.String("expected-repository"))
	if expectedRepository != "" {
		return expectedRepository, nil
	}

	if serverHost == "" || repository == "" {
		return "", fmt.Errorf("base images: --expected-repository or both --server-url and --repository are required: %w", errs.ErrUsage)
	}

	return defaultReleaseImageRepository(serverHost, repository) + repositorySuffix, nil
}

// baseImagesExpectedSource resolves the source repository URL expected in
// SLSA lineage: the explicit --expected-source flag, else
// <server-url>/<repository>.
func baseImagesExpectedSource(cmd *cli.Command, serverURL, repository string) (string, error) {
	expectedSource := strings.TrimSpace(cmd.String("expected-source"))
	if expectedSource != "" {
		return expectedSource, nil
	}

	if serverURL == "" || repository == "" {
		return "", fmt.Errorf("base images: --expected-source or both --server-url and --repository are required: %w", errs.ErrUsage)
	}

	return serverURL + "/" + repository, nil
}

// baseImagesPublicKeyFromCmd reads and validates the trusted Cosign public
// key flags when the verb requires them; otherwise both values stay empty.
func baseImagesPublicKeyFromCmd(cmd *cli.Command, requirePublicKey bool) (string, string, error) {
	if !requirePublicKey {
		return "", "", nil
	}

	publicKeyPath := strings.TrimSpace(cmd.String("cosign-public-key-path"))
	if unsafeWorkflowPath(publicKeyPath) {
		return "", "", fmt.Errorf("base images: unsafe Cosign public key path: %s: %w", publicKeyPath, errs.ErrUsage)
	}

	return publicKeyPath, strings.TrimSpace(cmd.String("cosign-public-key-sha256")), nil
}

func (c baseImagesCommon) login(authFile string) error {
	if c.RegistryUsername == "" {
		return errs.CredentialRequired(errs.Credential{What: credRegistryUsername, Env: envRegistryUser})
	}

	password, err := releaseImagesSecret(c.RegistryPassword, "REGISTRY_TOKEN", "REGISTRY_PASSWORD")
	if err != nil {
		return err
	}

	if password == "" {
		return errs.CredentialRequired(errs.Credential{What: credRegistryPassword, Flag: flagRegistryPasswordFile, Env: "REGISTRY_TOKEN"})
	}

	return appcontainer.RegistryLogin(os.Stderr, appcontainer.RegistryLoginInput{
		Registry: c.Registry,
		Username: c.RegistryUsername,
		Password: password,
		AuthFile: authFile,
	})
}

func withBaseImagesDockerConfig(fn func(authFile string) error) error {
	root := os.Getenv("RUNNER_TEMP")
	if root == "" {
		root = os.TempDir()
	}

	dir, err := os.MkdirTemp(root, "reusable-ci-base-images-docker-config-*")
	if err != nil {
		return fmt.Errorf("base images: create Docker config dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }() //nolint:gosec // dir is a fresh MkdirTemp path under the runner temp root, not caller input.

	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 0o700: a directory needs the owner execute bit; it stays owner-only.
		return fmt.Errorf("base images: chmod Docker config dir: %w", err)
	}

	oldDockerConfig, hadDockerConfig := os.LookupEnv("DOCKER_CONFIG")

	if err := os.Setenv("DOCKER_CONFIG", dir); err != nil {
		return fmt.Errorf("base images: set DOCKER_CONFIG: %w", err)
	}

	defer restoreEnv("DOCKER_CONFIG", oldDockerConfig, hadDockerConfig)

	authFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(authFile, []byte("{\"auths\":{}}\n"), 0o600); err != nil { //nolint:gosec // path is inside the fresh MkdirTemp dir created above.
		return fmt.Errorf("base images: write empty Docker config: %w", err)
	}

	return fn(authFile)
}

func validateBaseImagesPublicKey(path, want string) error {
	if !baseImagesHex64RE.MatchString(want) {
		return fmt.Errorf("base images: cosign-public-key-sha256 must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("base images: Cosign public key is missing, not a regular file, or a symlink: %s: %w", path, errs.ErrMissingInput)
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected public key path validated as a relative regular file.
	if err != nil {
		return fmt.Errorf("base images: read Cosign public key %s: %w", path, err)
	}

	sum := sha256.Sum256(body)

	actual := hex.EncodeToString(sum[:])
	if actual != want {
		return fmt.Errorf("base images: Cosign public key digest does not match expected trust root\n  path:     %s\n  expected: %s\n  actual:   %s: %w", path, want, actual, errs.ErrValidation)
	}

	return nil
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

func parseBaseInputs(raw string) ([]appcontainer.BaseInput, error) {
	var inputs []appcontainer.BaseInput
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil, fmt.Errorf("base images: base-inputs-json must be a JSON array: %w: %w", err, errs.ErrMalformedInput)
	}

	if inputs == nil {
		return nil, fmt.Errorf("base images: base-inputs-json must be a JSON array: %w", errs.ErrMalformedInput)
	}

	return inputs, nil
}

func readBaseCollectVerifiedImages(raw, file string) ([]appcontainer.BaseImageCollectImage, error) {
	if strings.TrimSpace(file) != "" {
		images, err := readBaseCollectImageFile(file, true)
		if err != nil {
			return nil, err
		}

		if len(images) > 0 {
			return images, nil
		}
	}

	return parseBaseCollectImages(raw, "verified-images-json")
}

func baseCollectBuiltFiles(files []string, dir, pattern string) ([]string, error) {
	paths := make([]string, 0, len(files))
	seen := map[string]bool{}

	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}

		seen[path] = true
		paths = append(paths, path)
	}
	for _, file := range files {
		add(file)
	}

	if strings.TrimSpace(dir) != "" {
		if strings.TrimSpace(pattern) == "" {
			pattern = "base-image-*.json"
		}

		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("base images collect: invalid built metadata glob: %w: %w", err, errs.ErrUsage)
		}

		for _, match := range matches {
			add(match)
		}
	}

	sort.Strings(paths)

	return paths, nil
}

func readBaseCollectImageFiles(files []string, allowMissing bool) ([]appcontainer.BaseImageCollectImage, error) {
	images := make([]appcontainer.BaseImageCollectImage, 0)

	for _, file := range files {
		items, err := readBaseCollectImageFile(file, allowMissing)
		if err != nil {
			return nil, err
		}

		images = append(images, items...)
	}

	return images, nil
}

func readBaseCollectImageFile(path string, allowMissing bool) ([]appcontainer.BaseImageCollectImage, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return []appcontainer.BaseImageCollectImage{}, nil
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected CI artifact metadata path.
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			return []appcontainer.BaseImageCollectImage{}, nil
		}

		return nil, fmt.Errorf("base images collect: read metadata file %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return []appcontainer.BaseImageCollectImage{}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		return parseBaseCollectImages(trimmed, path)
	}

	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("base images collect: metadata file must contain a JSON object or array: %s: %w", path, errs.ErrMalformedInput)
	}

	var image appcontainer.BaseImageCollectImage
	if err := json.Unmarshal([]byte(trimmed), &image); err != nil {
		return nil, fmt.Errorf("base images collect: metadata file must contain a JSON object: %s: %w: %w", path, err, errs.ErrMalformedInput)
	}

	return []appcontainer.BaseImageCollectImage{image}, nil
}

func parseBaseCollectImages(raw, label string) ([]appcontainer.BaseImageCollectImage, error) {
	if strings.TrimSpace(raw) == "" {
		return []appcontainer.BaseImageCollectImage{}, nil
	}

	var images []appcontainer.BaseImageCollectImage
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return nil, fmt.Errorf("base images collect: %s must be a JSON array: %w: %w", label, err, errs.ErrMalformedInput)
	}

	if images == nil {
		return nil, fmt.Errorf("base images collect: %s must be a JSON array: %w", label, errs.ErrMalformedInput)
	}

	return images, nil
}

func parseBaseCollectFlavors(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}

	var flavors []string
	if err := json.Unmarshal([]byte(raw), &flavors); err != nil {
		return nil, fmt.Errorf("base images collect: missing-flavors-json must be a JSON array of strings: %w: %w", err, errs.ErrMalformedInput)
	}

	if flavors == nil {
		return nil, fmt.Errorf("base images collect: missing-flavors-json must be a JSON array of strings: %w", errs.ErrMalformedInput)
	}

	return flavors, nil
}

func readBaseArchMetadata(path string) (appcontainer.BaseArchMetadata, error) {
	body, err := cliio.ReadFile(path)
	if err != nil {
		return appcontainer.BaseArchMetadata{}, fmt.Errorf("base images: read arch metadata: %w", err)
	}

	var metadata appcontainer.BaseArchMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return appcontainer.BaseArchMetadata{}, fmt.Errorf("base images: arch metadata must be a JSON object: %w: %w", err, errs.ErrMalformedInput)
	}

	return metadata, nil
}

func parseBaseImages(raw string) ([]appcontainer.BaseImageMetadata, error) {
	var images []appcontainer.BaseImageMetadata
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return nil, fmt.Errorf("base images: all-images-json must be a JSON array: %w: %w", err, errs.ErrMalformedInput)
	}

	if images == nil {
		return nil, fmt.Errorf("base images: all-images-json must be a JSON array: %w", errs.ErrMalformedInput)
	}

	return images, nil
}

func writeBaseImagesVerifyOutputs(ctx context.Context, dep *deps.Deps, result appcontainer.BaseImageVerifyExistingResult) error {
	if err := dep.OutputSink.SetBool(ctx, "all_found", result.AllFound); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "all_images_json", result.AllImagesJSON); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "missing_flavors_json", result.MissingFlavorsJSON)
}

func writeBaseImagesPromoteOutputs(ctx context.Context, dep *deps.Deps, result appcontainer.BaseImagePromoteResult) error {
	if err := dep.OutputSink.Set(ctx, "base_input_id", result.BaseInputID); err != nil {
		return err
	}

	if err := dep.OutputSink.Set(ctx, "base_images_json", result.BaseImagesJSON); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "base_input_ids_json", result.BaseInputIDsJSON)
}

func unsafeWorkflowPath(path string) bool {
	return path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") || strings.ContainsAny(path, "\n\r")
}
