// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	appbaseimages "github.com/diggsweden/reusable-ci/v3/internal/app/baseimages"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
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
			baseImagesVerifyExistingCmd(),
			baseImagesPromoteCmd(),
			baseImagesCleanupStagingCmd(),
			baseImagesFreshnessCmd(),
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

	auth := regflags.Resolve(cmd)

	return auth.Username, auth.PasswordFile
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
	password, err := regflags.LoginPassword(c.RegistryUsername, c.RegistryPassword)
	if err != nil {
		return err
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

func parseBaseInputs(raw string) ([]appbaseimages.BaseInput, error) {
	var inputs []appbaseimages.BaseInput
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil, fmt.Errorf("base images: base-inputs-json must be a JSON array: %w: %w", err, errs.ErrMalformedInput)
	}

	if inputs == nil {
		return nil, fmt.Errorf("base images: base-inputs-json must be a JSON array: %w", errs.ErrMalformedInput)
	}

	return inputs, nil
}

func parseBaseImages(raw string) ([]appbaseimages.BaseImageMetadata, error) {
	var images []appbaseimages.BaseImageMetadata
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return nil, fmt.Errorf("base images: all-images-json must be a JSON array: %w: %w", err, errs.ErrMalformedInput)
	}

	if images == nil {
		return nil, fmt.Errorf("base images: all-images-json must be a JSON array: %w", errs.ErrMalformedInput)
	}

	return images, nil
}

func unsafeWorkflowPath(path string) bool {
	return path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") || strings.ContainsAny(path, "\n\r")
}
