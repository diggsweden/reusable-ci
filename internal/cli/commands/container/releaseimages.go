// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

const (
	releaseImagesDefaultJournal = "image-promotions.jsonl"
	releaseImagesDefaultEnvVar  = "REGISTRY_TOKEN"
)

type releaseImagesCommon struct {
	DistDir                 string
	Ledger                  string
	ReleaseTag              string
	ServerURL               string
	Registry                string
	Repository              string
	RegistryUsername        string
	RegistryPasswordFile    string
	ExpectedImageRepository string
}

func releaseImagesGroup() *cli.Command {
	return &cli.Command{
		Name:  "release-images",
		Usage: "high-level release-image signing, promotion, rollback, and cleanup boundary",
		Description: `Workflow-facing release-image boundary. These commands combine the
digest-first image ledger with short-lived registry auth, signer-safe cosign
environment isolation, Forgejo package-API tag deletion, and forgejo-ci's
release-only promotion policy. Lower-level ` + "`container ledger`" + ` verbs remain
available for custom workflows; reusable workflows should use this group.`,
		Commands: []*cli.Command{
			releaseImagesSignCmd(),
			releaseImagesVerifyCmd(),
			releaseImagesPromoteCmd(),
			releaseImagesRollbackCmd(),
			releaseImagesCleanupCmd(),
		},
	}
}

func releaseImagesSignCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdSign,
		Usage: "sign and attest release images from the confined release-image ledger",
		Flags: append(
			append(
				releaseImagesCommonFlags(),
				&cli.StringFlag{Name: flagProvenanceEnvelope, Sources: cli.EnvVars("SLSA_PROVENANCE_ENVELOPE"), Usage: "in-toto statement JSON; defaults to <dist-dir>/slsa-provenance.intoto.json"},
				&cli.BoolFlag{Name: flagRecursive, Sources: cli.EnvVars("LEDGER_SIGN_RECURSIVE"), Usage: "pass --recursive to cosign sign/attest for manifest-list children"},
				&cli.StringFlag{Name: flagExpectedBaseRepository, Sources: cli.EnvVars("LEDGER_SIGN_EXPECTED_BASE_REPOSITORY"), Usage: "exact base image repository allowed for ledger base_ref values (default: <expected-image-repository>-base)"},
				&cli.StringFlag{Name: flagSBOMPathPattern, Sources: cli.EnvVars("LEDGER_SIGN_SBOM_PATH_PATTERN"), Usage: "regular expression every ledger SBOM path must match (default: <dist-dir>/image-sbom*.cyclonedx.json)"},
			),
			signflags.Cosign(signflags.CosignOpts{MethodNote: cosignMethodNoteGPG})...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := releaseImagesCommonFromCmd(cmd)
			if err != nil {
				return err
			}

			if stageErr := requireReleaseStage(cmd.String("stage")); stageErr != nil {
				return stageErr
			}

			entries, err := releaseImagesLoadLedger(common, true)
			if err != nil {
				return err
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			provenanceEnvelope := cmd.String(flagProvenanceEnvelope)
			if provenanceEnvelope == "" {
				provenanceEnvelope = common.DistDir + "/slsa-provenance.intoto.json"
			}

			expectedBaseRepository := cmd.String(flagExpectedBaseRepository)
			if expectedBaseRepository == "" {
				expectedBaseRepository = common.ExpectedImageRepository + "-base"
			}

			sbomPathPattern := cmd.String(flagSBOMPathPattern)
			if sbomPathPattern == "" {
				sbomPathPattern = "^" + regexp.QuoteMeta(common.DistDir) + `/image-sbom[-A-Za-z0-9_.]*[.]cyclonedx[.]json$`
			}

			return withReleaseImagesDockerConfig(func(authFile string) error {
				if err := common.login(authFile); err != nil {
					return err
				}

				return runLedgerSign(ctx,
					cosign.NewIsolated("COSIGN_KEY", "COSIGN_PASSWORD", "DOCKER_CONFIG"),
					ociregistry.WithAuthFile(authFile),
					appcontainer.SignLedgerImagesInput{
						Entries:                 entries,
						ReleaseTag:              common.ReleaseTag,
						PredicateEnvelopePath:   provenanceEnvelope,
						Method:                  method,
						Recursive:               cmd.Bool(flagRecursive),
						KeyRef:                  cmd.String("key"),
						OIDCIssuer:              cmd.String("oidc-issuer"),
						ExpectedImageRepository: common.ExpectedImageRepository,
						ExpectedBaseRepository:  expectedBaseRepository,
						SBOMPathPattern:         sbomPathPattern,
					})
			})
		},
	}
}

func releaseImagesPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdPromote,
		Usage: "promote signed release image candidates to their final/moving release tags",
		Flags: append(
			releaseImagesCommonFlags(),
			releaseImagesStateFlags(
				dryRunFlag(),
			)...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := releaseImagesCommonFromCmd(cmd)
			if err != nil {
				return err
			}

			if stageErr := requireReleaseStage(cmd.String("stage")); stageErr != nil {
				return stageErr
			}

			entries, err := releaseImagesLoadLedger(common, true)
			if err != nil {
				return err
			}

			journal, err := releaseImagesJournal(cmd)
			if err != nil {
				return err
			}

			return withReleaseImagesDockerConfig(func(authFile string) error {
				return releaseImagesPromote(ctx, cmd, common, entries, journal, authFile)
			})
		},
	}
}

// releaseImagesPromote runs the promote verb inside the short-lived Docker
// config: log in, journal the plan, then promote every ledger entry via the
// shared ledger promote body.
func releaseImagesPromote(ctx context.Context, cmd *cli.Command, common releaseImagesCommon, entries []imageledger.Entry, journal, authFile string) error {
	if err := common.login(authFile); err != nil {
		return err
	}

	stage := imageledger.Stage{
		Name:                   imageledger.ReleaseStageName,
		UseEntryReleaseTags:    true,
		AllowDigestRefFallback: true,
	}

	// Release promotion never rehomes registries, so no signature copier.
	reg, sigCopier := promotionRegistries(ociregistry.WithAuthFile(authFile), dryrun.Enabled(cmd), nil)

	return runLedgerPromotion(ctx, promotionRun{
		reg:            reg,
		sigCopier:      sigCopier,
		entries:        entries,
		releaseTag:     common.ReleaseTag,
		stage:          stage,
		journal:        journal,
		journalDirPerm: 0o700,
		errPrefix:      "release images",
		done:           fmt.Sprintf("release images: promoted %d entr(y/ies) to release tags", len(entries)),
		out:            os.Stderr,
	})
}

func releaseImagesRollbackCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRollback,
		Usage: "roll back a failed release-image promotion from its promotion journal",
		Flags: append(
			releaseImagesCommonFlags(),
			releaseImagesStateFlags(
				releaseImagesProviderTokenFileFlag(),
				dryRunFlag(),
			)...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := releaseImagesCommonFromCmd(cmd)
			if err != nil {
				return err
			}

			if stageErr := requireReleaseStage(cmd.String("stage")); stageErr != nil {
				return stageErr
			}

			journal, err := releaseImagesJournal(cmd)
			if err != nil {
				return err
			}

			if !fileHasContent(journal) {
				_, _ = fmt.Fprintln(os.Stderr, "No promoted image tags to roll back.")

				return nil
			}

			records, err := loadPromotionJournal(journal, "release images", common.ExpectedImageRepository)
			if err != nil {
				return err
			}

			return withReleaseImagesDockerConfig(func(authFile string) error {
				if err := common.login(authFile); err != nil {
					return err
				}

				if dryrun.Enabled(cmd) {
					reg := newDryRunRegistry(ociregistry.WithAuthFile(authFile), os.Stderr)

					return runPromotionJournalRollback(ctx, reg, records, common.ReleaseTag, "release images")
				}

				return releaseImagesWithTagDeleter(ctx, cmd, common, func(deleter imageledger.TagDeleter) error {
					reg := auditPromotionRollbackRegistry{
						PromotionRollbackRegistry: promotionRollbackRegistry{Registry: ociregistry.WithAuthFile(authFile), deleter: deleter},
						out:                       os.Stderr,
					}

					return runPromotionJournalRollback(ctx, reg, records, common.ReleaseTag, "release images")
				})
			})
		},
	}
}

func releaseImagesCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCleanup,
		Usage: "delete release candidate tags after verified final/moving promotion",
		Flags: append(
			releaseImagesCommonFlags(),
			releaseImagesProviderTokenFileFlag(),
			dryRunFlag(),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			common, err := releaseImagesCommonFromCmd(cmd)
			if err != nil {
				return err
			}

			if stageErr := requireReleaseStage(cmd.String("stage")); stageErr != nil {
				return stageErr
			}

			entries, err := releaseImagesLoadLedger(common, false)
			if err != nil {
				return err
			}

			return withReleaseImagesDockerConfig(func(authFile string) error {
				if err := common.login(authFile); err != nil {
					return err
				}

				if dryrun.Enabled(cmd) {
					reg := newDryRunRegistry(ociregistry.WithAuthFile(authFile), os.Stderr)

					return runLedgerCleanup(ctx, reg, entries, common.ReleaseTag, "release images")
				}

				return releaseImagesWithTagDeleter(ctx, cmd, common, func(deleter imageledger.TagDeleter) error {
					reg := auditDeleter{
						CleanupRegistry: cleanupRegistry{resolver: ociregistry.WithAuthFile(authFile), deleter: deleter},
						out:             os.Stderr,
					}

					return runLedgerCleanup(ctx, reg, entries, common.ReleaseTag, "release images")
				})
			})
		},
	}
}

func releaseImagesCommonFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "dist-dir", Value: "dist", Sources: cli.EnvVars("DIST_DIR"), Usage: "release dist directory; the default ledger and SLSA envelope are resolved under this directory"},
		&cli.StringFlag{Name: flagLedger, Sources: cli.EnvVars("RELEASE_IMAGES_PATH", "RELEASE_IMAGES_LEDGER"), Usage: "release-image ledger path; defaults to <dist-dir>/release-images.json and must remain under <dist-dir>/"},
		releaseTagFlag(),
		stageFlag(),
		&cli.StringFlag{Name: flagServerURL, Sources: cienv.ServerURL(), Usage: "forge server URL used to derive the registry host and provider API base"},
		&cli.StringFlag{Name: flagRepository, Sources: cienv.Repository(), Usage: "owner/repo used to derive the expected image repository"},
		&cli.StringFlag{Name: flagRegistry, Sources: cli.EnvVars("CONTAINER_REGISTRY"), Usage: "registry host for the short-lived Docker auth config (default: host from --server-url)"},
		regflags.Username(),
		regflags.PasswordFile(),
		expectedImageRepositoryFlag(),
	}
}

func releaseImagesStateFlags(extra ...cli.Flag) []cli.Flag {
	flags := make([]cli.Flag, 0, 2+len(extra)) //nolint:mnd // the two fixed state flags appended below.
	flags = append(flags,
		&cli.StringFlag{Name: "state-dir", Sources: cli.EnvVars("SIGN_AND_PUBLISH_STATE_DIR"), Usage: "release-image state directory; default promotion journal is <state-dir>/image-promotions.jsonl"},
		promotionJournalFlag(),
	)

	return append(flags, extra...)
}

func releaseImagesProviderTokenFileFlag() cli.Flag {
	return &cli.StringFlag{Name: "provider-token-file", Usage: "file containing the provider/package-API token (\"-\" reads stdin); defaults to $REUSABLE_CI_PROVIDER_TOKEN / provider-native token env. The token never appears in argv."}
}

func releaseImagesCommonFromCmd(cmd *cli.Command) (releaseImagesCommon, error) {
	distDir := strings.TrimRight(cmd.String("dist-dir"), "/")
	if distDir == "" {
		return releaseImagesCommon{}, fmt.Errorf("release images: --dist-dir is required: %w", errs.ErrUsage)
	}

	ledger := cmd.String(flagLedger)
	if ledger == "" {
		ledger = distDir + "/" + defaultLedgerBasename
	}

	if err := validateReleaseImagesPath(ledger, distDir); err != nil {
		return releaseImagesCommon{}, err
	}

	serverURL := normalizedServerURL(cmd.String(flagServerURL))

	serverHost, err := optionalRegistryHost(serverURL)
	if err != nil {
		return releaseImagesCommon{}, err
	}

	registry, err := resolveRegistryHostFromFlags(cmd, serverHost)
	if err != nil {
		return releaseImagesCommon{}, err
	}

	if registry == "" {
		return releaseImagesCommon{}, fmt.Errorf("release images: --registry or --server-url is required for registry login: %w", errs.ErrUsage)
	}

	repository := strings.TrimSpace(cmd.String(flagRepository))

	expectedImageRepository, err := releaseImagesExpectedRepository(cmd, serverHost, repository)
	if err != nil {
		return releaseImagesCommon{}, err
	}

	return releaseImagesCommon{
		DistDir:                 distDir,
		Ledger:                  ledger,
		ReleaseTag:              cmd.String(flagTag),
		ServerURL:               serverURL,
		Registry:                registry,
		Repository:              repository,
		RegistryUsername:        strings.TrimSpace(cmd.String(flagRegistryUsername)),
		RegistryPasswordFile:    cmd.String(flagRegistryPasswordFile),
		ExpectedImageRepository: expectedImageRepository,
	}, nil
}

// resolveRegistryHostFromFlags picks the registry host for short-lived
// auth: --registry when set (normalized to a bare host), else the host
// derived from --server-url; empty when neither is available.
func resolveRegistryHostFromFlags(cmd *cli.Command, serverHost string) (string, error) {
	registry := cmd.String(flagRegistry)
	if registry == "" {
		return serverHost, nil
	}

	return registryHost(registry)
}

// releaseImagesExpectedRepository resolves the exact release-image
// repository: the explicit --expected-image-repository flag, else
// host/lower(owner/repo).
func releaseImagesExpectedRepository(cmd *cli.Command, serverHost, repository string) (string, error) {
	expectedImageRepository := strings.TrimSpace(cmd.String(flagExpectedImageRepository))
	if expectedImageRepository != "" {
		return expectedImageRepository, nil
	}

	if serverHost == "" || repository == "" {
		return "", fmt.Errorf("release images: --expected-image-repository or both --server-url and --repository are required: %w", errs.ErrUsage)
	}

	return defaultReleaseImageRepository(serverHost, repository), nil
}

func validateReleaseImagesPath(path, distDir string) error {
	if path == "" {
		return fmt.Errorf("release images: ledger path is required: %w", errs.ErrUsage)
	}

	distDir = strings.TrimRight(distDir, "/")
	if distDir == "" {
		return fmt.Errorf("release images: dist dir is required: %w", errs.ErrUsage)
	}

	if path == distDir || !strings.HasPrefix(path, distDir+"/") {
		return fmt.Errorf("release images: ledger path must stay under %s/: %s: %w", distDir, path, errs.ErrUsage)
	}

	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return fmt.Errorf("release images: ledger path must not contain '..': %s: %w", path, errs.ErrUsage)
		}
	}

	return nil
}

func defaultReleaseImageRepository(serverHost, repository string) string {
	return serverHost + "/" + strings.ToLower(repository)
}

func normalizedServerURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" || strings.Contains(raw, "://") {
		return raw
	}

	return "https://" + raw
}

func optionalRegistryHost(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	return registryHost(raw)
}

func registryHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")

	raw = strings.Trim(raw, "/")
	if slash := strings.Index(raw, "/"); slash >= 0 {
		raw = raw[:slash]
	}

	if raw == "" {
		return "", fmt.Errorf("release images: registry host is empty: %w", errs.ErrUsage)
	}

	return raw, nil
}

func releaseImagesLoadLedger(common releaseImagesCommon, nonEmpty bool) ([]imageledger.Entry, error) {
	data, err := cliio.ReadFile(common.Ledger)
	if err != nil {
		return nil, fmt.Errorf("release images: read ledger %s: %w", common.Ledger, err)
	}

	entries, err := imageledger.Parse(data)
	if err != nil {
		return nil, err
	}

	if err := imageledger.ValidateAll(entries, common.ReleaseTag); err != nil {
		return nil, err
	}

	if nonEmpty && len(entries) == 0 {
		return nil, fmt.Errorf("imageledger: ledger must contain at least one entry: %w", errs.ErrValidation)
	}

	if err := validateEntryRepositories(entries, common.ExpectedImageRepository); err != nil {
		return nil, err
	}

	return entries, nil
}

func (c releaseImagesCommon) login(authFile string) error {
	if c.RegistryUsername == "" {
		return errs.CredentialRequired(errs.Credential{What: credRegistryUsername, Env: envRegistryUser})
	}

	password, err := releaseImagesSecret(c.RegistryPasswordFile, releaseImagesDefaultEnvVar, "REGISTRY_PASSWORD")
	if err != nil {
		return err
	}

	if password == "" {
		return errs.CredentialRequired(errs.Credential{What: credRegistryPassword, Flag: flagRegistryPasswordFile, Env: releaseImagesDefaultEnvVar})
	}

	return appcontainer.RegistryLogin(os.Stderr, appcontainer.RegistryLoginInput{
		Registry: c.Registry,
		Username: c.RegistryUsername,
		Password: password,
		AuthFile: authFile,
	})
}

func releaseImagesSecret(filePath string, envVars ...string) (string, error) {
	if filePath != "" {
		return secret.Resolve(filePath, "")
	}

	for _, envVar := range envVars {
		if value := strings.TrimRight(os.Getenv(envVar), "\r\n"); value != "" {
			return value, nil
		}
	}

	return "", nil
}

func withReleaseImagesDockerConfig(fn func(authFile string) error) error {
	root := os.Getenv("RUNNER_TEMP")
	if root == "" {
		root = os.TempDir()
	}

	dir, err := os.MkdirTemp(root, "reusable-ci-release-images-docker-config-*")
	if err != nil {
		return fmt.Errorf("release images: create Docker config dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }() //nolint:gosec // dir is a fresh MkdirTemp path under the runner temp root, not caller input.

	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 0o700: a directory needs the owner execute bit; it stays owner-only.
		return fmt.Errorf("release images: chmod Docker config dir: %w", err)
	}

	oldDockerConfig, hadDockerConfig := os.LookupEnv("DOCKER_CONFIG")

	if err := os.Setenv("DOCKER_CONFIG", dir); err != nil {
		return fmt.Errorf("release images: set DOCKER_CONFIG: %w", err)
	}

	defer restoreEnv("DOCKER_CONFIG", oldDockerConfig, hadDockerConfig)

	return fn(filepath.Join(dir, "config.json"))
}

func releaseImagesJournal(cmd *cli.Command) (string, error) {
	if journal := cmd.String("journal"); journal != "" {
		return journal, nil
	}

	stateDir := cmd.String("state-dir")
	if stateDir == "" {
		return "", fmt.Errorf("release images: --journal or --state-dir is required: %w", errs.ErrUsage)
	}

	return filepath.Join(stateDir, releaseImagesDefaultJournal), nil
}

func fileHasContent(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Size() > 0
}

func releaseImagesWithTagDeleter(ctx context.Context, cmd *cli.Command, common releaseImagesCommon, fn func(imageledger.TagDeleter) error) error {
	token, err := releaseImagesSecret(cmd.String("provider-token-file"), "REUSABLE_CI_PROVIDER_TOKEN", "FORGEJO_TOKEN", "GITEA_TOKEN", "GITHUB_TOKEN")
	if err != nil {
		return err
	}

	if token == "" {
		return errs.CredentialRequired(errs.Credential{What: "provider tag-deletion token", Flag: "provider-token-file", Env: "REUSABLE_CI_PROVIDER_TOKEN"})
	}

	return withReleaseImagesProviderEnv(common, token, func() error {
		return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			deleter, err := d.RequireTagDeleter()
			if err != nil {
				return err
			}

			return fn(deleter)
		})
	})
}

func withReleaseImagesProviderEnv(common releaseImagesCommon, token string, fn func() error) error {
	return withEnv(map[string]string{
		"FORGEJO_TOKEN":      token,
		"FORGEJO_SERVER_URL": common.ServerURL,
		"FORGEJO_REPOSITORY": common.Repository,
	}, fn)
}

func withEnv(values map[string]string, fn func() error) error {
	type oldValue struct {
		value string
		had   bool
	}

	old := make(map[string]oldValue, len(values))
	for key, value := range values {
		if value == "" {
			continue
		}

		oldVal, had := os.LookupEnv(key)

		old[key] = oldValue{value: oldVal, had: had}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("release images: set %s: %w", key, err)
		}
	}

	defer func() {
		for key, oldVal := range old {
			restoreEnv(key, oldVal.value, oldVal.had)
		}
	}()

	return fn()
}

func restoreEnv(key, value string, had bool) {
	if had {
		_ = os.Setenv(key, value)

		return
	}

	_ = os.Unsetenv(key)
}

func requireReleaseStage(stage string) error {
	if stage != imageledger.ReleaseStageName {
		return fmt.Errorf("release images: only --stage release is supported: %w", errs.ErrUsage)
	}

	return nil
}
