// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"regexp"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/regflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// defaultLedgerBasename is the conventional release-image ledger filename;
// defaultLedgerPath places it under the GoReleaser dist layout. Overridable
// via --ledger. The basename is also what `ledger merge` looks for when handed
// a directory of downloaded per-container ledger artifacts.
const (
	defaultLedgerBasename = "release-images.json"
	defaultLedgerPath     = "dist/" + defaultLedgerBasename
)

var releaseTagFromImageTagRE = regexp.MustCompile(`^(v[0-9]+[.][0-9]+[.][0-9]+)(?:-|$)`) //nolint:gochecknoglobals // compiled regex table; read-only.

// The ledger verbs are split one file per concern: ledgeradd.go,
// ledgermerge.go, ledgervalidate.go, ledgersign.go, ledgerpromote.go,
// ledgercleanup.go, and ledgerrollback.go, with the registry decorators
// in ledgerregistries.go. Shared flags and multi-verb helpers live here.
//
// ledgerGroup wires `container ledger`: the digest-first release-image
// ledger — record image entries from the build stage and re-validate the
// ledger at the sign/publish trust boundary. Forge-agnostic (pure OCI
// refs/digests/tags); the Go port of forgejo-ci's record-release-image.
func ledgerGroup() *cli.Command {
	return &cli.Command{
		Name:  flagLedger,
		Usage: "release-image ledger: record image entries and re-validate at the trust boundary",
		Commands: []*cli.Command{
			ledgerAddCmd(),
			ledgerMergeCmd(),
			ledgerValidateCmd(),
			ledgerVerifyDigestsCmd(),
			ledgerSignCmd(),
			ledgerPromoteCmd(),
			ledgerCleanupCmd(),
			ledgerRollbackCmd(),
		},
	}
}

// ledgerEntriesFromCmd is the shared entry-loading prologue of the
// mutating ledger verbs: read the --ledger file, parse it, and enforce
// the optional --expected-image-repository confinement.
func ledgerEntriesFromCmd(cmd *cli.Command) ([]imageledger.Entry, error) {
	data, err := cliio.ReadFile(cmd.String(flagLedger))
	if err != nil {
		return nil, fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
	}

	entries, err := imageledger.Parse(data)
	if err != nil {
		return nil, err
	}

	if repoErr := validateEntryRepositories(entries, cmd.String(flagExpectedImageRepository)); repoErr != nil {
		return nil, repoErr
	}

	return entries, nil
}

// loadPromotionJournal reads + parses a promotion rollback journal and
// enforces the optional expected-repository confinement on every
// record. errPrefix names the calling boundary ("ledger" or "release
// images").
func loadPromotionJournal(journal, errPrefix, expectedRepository string) ([]imageledger.PromotionRecord, error) {
	data, err := cliio.ReadFile(journal)
	if err != nil {
		return nil, fmt.Errorf("%s: read promotion journal %s: %w", errPrefix, journal, err)
	}

	records, err := imageledger.ParsePromotionJournal(data)
	if err != nil {
		return nil, err
	}

	if repoErr := validatePromotionRecordRepositories(records, expectedRepository); repoErr != nil {
		return nil, repoErr
	}

	return records, nil
}

func validateEntryRepositories(entries []imageledger.Entry, expectedRepository string) error {
	for idx, entry := range entries {
		if err := imageledger.ValidateEntryRepository(entry, expectedRepository); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}
	}

	return nil
}

func validatePromotionRecordRepositories(records []imageledger.PromotionRecord, expectedRepository string) error {
	for idx, record := range records {
		if err := imageledger.ValidatePromotionRecordRepository(record, expectedRepository); err != nil {
			return fmt.Errorf("imageledger: promotion journal entry %d: %w", idx, err)
		}
	}

	return nil
}

// dryRunFlag previews registry mutations without performing them. The flag
// spelling and usage template live in the shared internal/cli/dryrun package.
func dryRunFlag() cli.Flag {
	return dryrun.Flag("registry mutations (copies/deletes)")
}

func promotionJournalFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "journal",
		Sources: cli.EnvVars("IMAGE_PROMOTIONS_JOURNAL"),
		Usage:   "JSONL promotion rollback journal; promote writes it before tag moves, rollback restores/deletes from it",
	}
}

// ledgerAuthFileFlag is the shared --auth-file flag of every ledger verb that
// reaches a registry. The ledger family is the most registry-dependent part of
// the product — capture, verify, promote, cleanup and rollback all read or write
// one — so leaving them keychain-only made them the only container verbs whose
// credentials could not be stated explicitly. usage carries the verb-specific
// wording, matching how the image/manifest verbs declare the same flag.
func ledgerAuthFileFlag(usage string) cli.Flag {
	return regflags.AuthFile(regflags.AuthFileOpts{Usage: usage})
}

// ledgerRegistry builds the registry adapter for a ledger verb: an explicit
// --auth-file (or $REUSABLE_CI_REGISTRY_AUTH_FILE) when given, otherwise the
// ambient keychain, which is what a `docker login` in the job leaves behind.
func ledgerRegistry(cmd *cli.Command) *ociregistry.Adapter {
	if authFile := cmd.String(flagAuthFile); authFile != "" {
		return ociregistry.WithAuthFile(authFile)
	}

	return ociregistry.New()
}

func releaseTagFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    flagTag,
		Sources: cienv.Tag(),
		Usage:   "release tag that final_tag (and candidate_tag) must be scoped to",
	}
}

func stageFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "stage",
		Value:   imageledger.ReleaseStageName,
		Sources: cli.EnvVars("PROMOTE_STAGE"),
		Usage:   "promotion stage: 'release' adds the <base>:release pointer and enforces the release scope; a named stage ('dev', 'staging') adds <base>:<stage> on the same digest and needs no --tag. The immutable :<version> tag is build-only.",
	}
}

func stageRepoFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "stage-repo",
		Sources: cli.EnvVars("PROMOTE_STAGE_REPO"),
		Usage:   "rehome the promotion onto a destination registry/namespace PREFIX (e.g. a sovereign Forgejo host, e.g. forgejo.example.com/owner); each image lands at <prefix>/<image-name>, so a multi-container release never collides. A different registry is a cross-registry promotion that copies the signature via cosign",
	}
}

func releaseTagsFromLedgerFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "release-tags-from-ledger",
		Sources: cli.EnvVars("PROMOTE_RELEASE_TAGS_FROM_LEDGER"),
		Usage:   "for the release stage, promote to each entry's final_tag and optional moving_tag instead of the generic <base>:release pointer",
	}
}

func expectedImageRepositoryFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    flagExpectedImageRepository,
		Sources: cli.EnvVars("LEDGER_EXPECTED_IMAGE_REPOSITORY"),
		Usage:   "optional exact image repository allowed for ledger refs/tags at the signer/publisher boundary",
	}
}

func ledgerPathFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    flagLedger,
		Value:   defaultLedgerPath,
		Sources: cli.EnvVars("RELEASE_IMAGES_LEDGER"),
		Usage:   "ledger JSON file (a bare array; created if absent on add)",
	}
}

func ledgerTagRef(imageName, tagName string) string {
	return imageName + ":" + tagName
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
