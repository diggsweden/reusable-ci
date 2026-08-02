// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/syft"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/signflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
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

// cleanupRegistry composes the digest resolver (docker, OCI-generic) and
// the tag deleter (the forge provider, via its package API) into
// imageledger.CleanupRegistry. Deletion is forge-specific on purpose:
// staging and final tags share one manifest (digest-preserving promote),
// so a generic OCI/skopeo delete would destroy the promoted image — only
// the forge's tag-scoped package-API delete is safe.
type cleanupRegistry struct {
	resolver imageledger.DigestResolver
	deleter  provider.TagDeleter
}

func (r cleanupRegistry) ResolveDigest(ctx context.Context, ref string) (string, error) {
	return r.resolver.ResolveDigest(ctx, ref)
}

func (r cleanupRegistry) DeleteTag(ctx context.Context, ref string) error {
	return r.deleter.DeleteTag(ctx, ref)
}

type promotionRollbackRegistry struct {
	imageledger.Registry
	deleter provider.TagDeleter
}

func (r promotionRollbackRegistry) DeleteTag(ctx context.Context, ref string) error {
	return r.deleter.DeleteTag(ctx, ref)
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

// runLedgerCleanup is the shared cleanup body for `ledger cleanup` and
// `release-images cleanup`: delete each entry's staging candidate tag
// (the domain verifies the promoted final tag first), then report.
// msgPrefix names the calling boundary ("ledger" or "release images").
func runLedgerCleanup(ctx context.Context, reg imageledger.CleanupRegistry, entries []imageledger.Entry, tag, msgPrefix string) error {
	if err := imageledger.Cleanup(ctx, reg, entries, tag); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "%s: cleaned up candidate tags for %d entr(y/ies) (release tag %q)\n", msgPrefix, len(entries), tag)

	return nil
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

// runPromotionJournalRollback is the shared rollback body for `ledger
// rollback --journal` and `release-images rollback`: undo the journaled
// promotion, then report with the caller's message prefix.
func runPromotionJournalRollback(ctx context.Context, reg imageledger.PromotionRollbackRegistry, records []imageledger.PromotionRecord, tag, msgPrefix string) error {
	if err := imageledger.RollbackPromotionJournal(ctx, reg, records, tag); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "%s: rolled back %d promotion journal entr(y/ies)\n", msgPrefix, len(records))

	return nil
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

// cleanupReg builds the registry the cleanup flow runs against. In
// dry-run it previews via the in-memory decorator (no forge needed); a
// real run resolves digests via docker and deletes via the active
// forge's tag-scoped package API (gated by RequireTagDeleter).
func cleanupReg(d *deps.Deps, dryRun bool) (imageledger.CleanupRegistry, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if dryRun {
		return newDryRunRegistry(ociregistry.New(), os.Stderr), nil
	}

	deleter, err := d.RequireTagDeleter()
	if err != nil {
		return nil, err
	}

	// Narrate each real deletion to stderr (audit trail), matching the
	// per-tag visibility the dry-run path already gives.
	return auditDeleter{
		CleanupRegistry: cleanupRegistry{resolver: ociregistry.New(), deleter: deleter},
		out:             os.Stderr,
	}, nil
}

func promotionRollbackReg(d *deps.Deps, dryRun bool) (imageledger.PromotionRollbackRegistry, error) { //nolint:varnamelen // idiomatic short name.
	if dryRun {
		return newDryRunRegistry(ociregistry.New(), os.Stderr), nil
	}

	deleter, err := d.RequireTagDeleter()
	if err != nil {
		return nil, err
	}

	return auditPromotionRollbackRegistry{
		PromotionRollbackRegistry: promotionRollbackRegistry{Registry: ociregistry.New(), deleter: deleter},
		out:                       os.Stderr,
	}, nil
}

// dryRunFlag previews registry mutations without performing them. The flag
// spelling and usage template live in the shared internal/cli/dryrun package.
func dryRunFlag() cli.Flag {
	return dryrun.Flag("registry mutations (copies/deletes)")
}

// dryRunRegistry previews ledger mutations without touching the
// registry: digest reads pass through to the wrapped resolver, a CopyTag
// is simulated in-memory (so the domain's post-copy digest verification
// still runs and passes), and both CopyTag/DeleteTag are logged. It
// implements imageledger.Registry and imageledger.CleanupRegistry.
type dryRunRegistry struct {
	resolver  imageledger.DigestResolver
	out       io.Writer
	simulated map[string]string
}

func newDryRunRegistry(resolver imageledger.DigestResolver, out io.Writer) *dryRunRegistry {
	return &dryRunRegistry{resolver: resolver, out: out, simulated: map[string]string{}}
}

func (d *dryRunRegistry) ResolveDigest(ctx context.Context, ref string) (string, error) {
	if dig, ok := d.simulated[ref]; ok {
		return dig, nil
	}

	return d.resolver.ResolveDigest(ctx, ref)
}

func (d *dryRunRegistry) CopyTag(ctx context.Context, source, dest string) error {
	dig, err := d.resolver.ResolveDigest(ctx, source)
	if err != nil {
		return err
	}

	d.simulated[dest] = dig
	_, _ = fmt.Fprintf(d.out, "[dry-run] would promote %s -> %s (%s)\n", source, dest, dig)

	return nil
}

func (d *dryRunRegistry) DeleteTag(_ context.Context, ref string) error {
	_, _ = fmt.Fprintf(d.out, "[dry-run] would delete %s\n", ref)

	return nil
}

// CopyWithSignatures simulates a cross-registry promotion (the real run
// uses cosign copy). It lands the source's digest at dest in the simulated
// map so the domain's post-copy digest verification still runs and passes.
func (d *dryRunRegistry) CopyWithSignatures(ctx context.Context, source, dest string) error {
	dig, err := d.resolver.ResolveDigest(ctx, source)
	if err != nil {
		return err
	}

	d.simulated[dest] = dig
	_, _ = fmt.Fprintf(d.out, "[dry-run] would cross-registry promote (with signatures) %s -> %s (%s)\n", source, dest, dig)

	return nil
}

// cosignSigCopier adapts the cosign adapter to imageledger.SignatureCopier:
// it narrates each cross-registry copy (matching the same-repo audit trail)
// and supplies the redaction sink. Used for real cross-registry promotion,
// where the registry-attached signature must travel with the image.
type cosignSigCopier struct {
	cosign *cosign.Adapter
	out    io.Writer
}

func (c cosignSigCopier) CopyWithSignatures(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(c.out, "ledger: cross-registry promoting (with signatures) %s -> %s\n", source, dest)

	return c.cosign.CopyImage(ctx, cosign.CopyImageInput{Source: source, Dest: dest}, c.out)
}

// auditCopier wraps the real promote registry and narrates each CopyTag
// to out before delegating. Mirrors dryRunRegistry's preview narration so
// a non-dry-run promotion leaves the same per-tag audit trail in the CI
// log that --dry-run shows — destructive registry mutations should be at
// least as observable as their preview. ResolveDigest passes through the
// embedded interface unlogged.
type auditCopier struct {
	imageledger.Registry

	out io.Writer
}

func (a auditCopier) CopyTag(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: promoting %s -> %s\n", source, dest)

	return a.Registry.CopyTag(ctx, source, dest)
}

// auditDeleter is the cleanup/rollback counterpart: it narrates each real
// DeleteTag before delegating, so the actually-destructive deletion is as
// observable as the [dry-run] preview.
type auditDeleter struct {
	imageledger.CleanupRegistry

	out io.Writer
}

func (a auditDeleter) DeleteTag(ctx context.Context, ref string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: deleting %s\n", ref)

	return a.CleanupRegistry.DeleteTag(ctx, ref)
}

type auditPromotionRollbackRegistry struct {
	imageledger.PromotionRollbackRegistry

	out io.Writer
}

func (a auditPromotionRollbackRegistry) CopyTag(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: restoring %s -> %s\n", source, dest)

	return a.PromotionRollbackRegistry.CopyTag(ctx, source, dest)
}

func (a auditPromotionRollbackRegistry) DeleteTag(ctx context.Context, ref string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: deleting %s\n", ref)

	return a.PromotionRollbackRegistry.DeleteTag(ctx, ref)
}

func promotionJournalFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "journal",
		Sources: cli.EnvVars("IMAGE_PROMOTIONS_JOURNAL"),
		Usage:   "JSONL promotion rollback journal; promote writes it before tag moves, rollback restores/deletes from it",
	}
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
		Usage:   "rehome the promotion onto a destination registry/namespace PREFIX (e.g. a sovereign codeberg.org/owner); each image lands at <prefix>/<image-name>, so a multi-container release never collides. A different registry is a cross-registry promotion that copies the signature via cosign",
	}
}

func releaseTagsFromLedgerFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "release-tags-from-ledger",
		Sources: cli.EnvVars("PROMOTE_RELEASE_TAGS_FROM_LEDGER"),
		Usage:   "for the release stage, promote to each entry's final_tag and optional moving_tag instead of the generic <base>:release pointer",
	}
}

func digestRefFallbackFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "allow-digest-ref-fallback",
		Sources: cli.EnvVars("PROMOTE_ALLOW_DIGEST_REF_FALLBACK"),
		Usage:   "when candidate_tag is absent or no longer serves the recorded digest, copy from the ledger ref digest instead (Forgejo release rerun recovery)",
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

func ledgerAddCmd() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "validate one image entry and append it to the ledger",
		Description: `EXAMPLES:
   # Record a pushed image, capturing its digest from the registry
   reusable-ci container ledger add --kind distroless \
     --candidate-tag codeberg.org/owner/repo:staging-v1.2.3 \
     --final-tag codeberg.org/owner/repo:v1.2.3 \
     --sbom dist/image-sbom.cyclonedx.json --tag v1.2.3 --capture-digest

   # Direct-push consumer with an explicit digest
   reusable-ci container ledger add --kind alpine \
     --ref codeberg.org/owner/repo@sha256:… --digest sha256:… \
     --final-tag codeberg.org/owner/repo:v1.2.3-alpine \
     --sbom dist/image-sbom-alpine.cyclonedx.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			&cli.StringFlag{Name: "kind", Usage: "image role, e.g. distroless, alpine, base"},
			&cli.StringFlag{Name: flagRef, Usage: "digest-pinned image ref (registry/path@sha256:<64 hex>)"},
			&cli.StringFlag{Name: flagDigest, Usage: "image digest (sha256:<64 hex>)"},
			&cli.StringFlag{Name: "sbom", Usage: "CycloneDX SBOM path (dist/image-sbom*.cyclonedx.json)"},
			&cli.StringFlag{Name: "image-name", Usage: "image registry/path (no tag); with --tag, derives --final-tag=<image>:<tag> and --candidate-tag=<image>:staging-<tag> so callers don't hand-assemble both (explicit flags still win)"}, //nolint:goconst // flag name; matches the package convention.
			&cli.StringFlag{Name: "final-tag-name", Usage: "immutable release tag name/portion combined with --image-name; when --tag is empty, derives the release tag from vMAJOR.MINOR.PATCH[-suffix]"},
			&cli.StringFlag{Name: "final-tag", Usage: "immutable release tag ref (scoped to --tag); derived from --image-name when omitted"},
			&cli.StringFlag{Name: "moving-tag-name", Usage: "optional moving tag name/portion combined with --image-name"},
			&cli.StringFlag{Name: "moving-tag", Usage: "optional moving tag ref, e.g. codeberg.org/owner/repo:rust"},
			&cli.StringFlag{Name: flagFlavor, Usage: "optional base-image flavour"},
			&cli.BoolFlag{Name: "derive-candidate-tag", Sources: cli.EnvVars("LEDGER_DERIVE_CANDIDATE_TAG"), Usage: "derive --candidate-tag as <image>:staging-<final-tag-name>; mutually exclusive with explicit candidate tag flags"},
			&cli.BoolFlag{Name: "default-sbom", Usage: "when --sbom is empty, use dist/image-sbom-<flavor|kind>.cyclonedx.json"},
			&cli.StringFlag{Name: "candidate-tag-name", Usage: "optional staging tag name/portion combined with --image-name"},
			&cli.StringFlag{Name: "candidate-tag", Usage: "optional staging tag ref (scoped to staging-<tag>); derived from --image-name when omitted"},
			&cli.StringFlag{Name: "base-ref", Usage: "optional digest-pinned base image"},
			&cli.StringFlag{Name: flagBaseInputID, Usage: "optional base-image input identifier (SLSA lineage)"},
			&cli.BoolFlag{Name: "capture-digest", Usage: "resolve the digest from the registry (single source of truth) instead of --ref/--digest; reads the candidate or final tag"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path := cmd.String(flagLedger)

			entry, releaseTag, err := ledgerAddEntryFromFlags(ledgerAddFlags{
				ReleaseTag:          cmd.String(flagTag),
				Kind:                cmd.String("kind"),
				ImageName:           cmd.String("image-name"),
				Ref:                 cmd.String(flagRef),
				Digest:              cmd.String(flagDigest),
				SBOM:                cmd.String("sbom"),
				DefaultSBOM:         cmd.Bool("default-sbom"),
				FinalTagName:        cmd.String("final-tag-name"),
				FinalTag:            cmd.String("final-tag"),
				MovingTagName:       cmd.String("moving-tag-name"),
				MovingTag:           cmd.String("moving-tag"),
				Flavor:              cmd.String(flagFlavor),
				DeriveCandidateTag:  cmd.Bool("derive-candidate-tag"),
				CandidateTagName:    cmd.String("candidate-tag-name"),
				CandidateTag:        cmd.String("candidate-tag"),
				BaseRef:             cmd.String("base-ref"),
				BaseInputID:         cmd.String(flagBaseInputID),
				LegacyImageNameMode: cmd.String("final-tag-name") == "" && cmd.String("final-tag") == "",
			})
			if err != nil {
				return err
			}

			// Digest capture hits the registry — do it before taking the
			// lock so we never hold the ledger lock across a network call.
			if cmd.Bool("capture-digest") {
				if err := captureEntryDigest(ctx, ociregistry.New(), &entry); err != nil {
					return err
				}
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // dist dir; 0755 is conventional.
				return fmt.Errorf("ledger: create dir for %s: %w", path, err)
			}

			// Serialise the read-modify-write under an exclusive file lock:
			// parallel `ledger add` against one ledger (e.g. several image
			// variants recorded concurrently) would otherwise clobber each
			// other and silently drop entries. Re-read inside the lock so
			// each writer sees the latest state.
			var added bool

			if err := cliio.WithLock(path, func() error {
				existing, _ := os.ReadFile(path) //nolint:gosec,errcheck // operator-supplied ledger path; an absent file means an empty ledger.

				out, wasNew, err := imageledger.Append(existing, entry, releaseTag)
				if err != nil {
					return err
				}

				added = wasNew

				return cliio.WriteFile(path, out, 0o644) //nolint:gosec // ledger is public release metadata, not a secret.
			}); err != nil {
				return err
			}

			// Report the truthful outcome: Append is idempotent, so a
			// retried add of an already-recorded entry is a no-op, not an
			// append.
			verb := "appended"
			if !added {
				verb = "already recorded; no change to"
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %s %s (%s) → %s\n", verb, entryDescriptor(entry.Kind), entry.Digest, path)

			return nil
		},
	}
}

// entryDescriptor renders the ledger entry's noun for the add message. --kind
// is optional, so an empty kind collapses to a bare "entry" rather than
// leaving a doubled space.
func entryDescriptor(kind string) string {
	if kind == "" {
		return "entry"
	}

	return kind + " entry"
}

type ledgerAddFlags struct {
	ReleaseTag          string
	Kind                string
	ImageName           string
	Ref                 string
	Digest              string
	SBOM                string
	DefaultSBOM         bool
	FinalTagName        string
	FinalTag            string
	MovingTagName       string
	MovingTag           string
	Flavor              string
	DeriveCandidateTag  bool
	CandidateTagName    string
	CandidateTag        string
	BaseRef             string
	BaseInputID         string
	LegacyImageNameMode bool
}

func ledgerAddEntryFromFlags(flags ledgerAddFlags) (imageledger.Entry, string, error) {
	imageName, err := ledgerAddImageName(flags.ImageName)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	flags, finalTag, finalTagName, err := ledgerAddFinalTag(flags, imageName)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	movingTag, err := ledgerAddMovingTag(flags, imageName)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	candidateTag, err := ledgerAddCandidateTag(flags, imageName, finalTagName)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	releaseTag, err := ledgerAddReleaseTag(flags.ReleaseTag, finalTagName)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	sbom, err := ledgerAddSBOM(flags)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	ref := flags.Ref
	if ref == "" && imageName != "" && flags.Digest != "" {
		ref = imageName + "@" + flags.Digest
	}

	return imageledger.Entry{
		Kind:         flags.Kind,
		Flavor:       flags.Flavor,
		Ref:          ref,
		Digest:       flags.Digest,
		SBOM:         sbom,
		FinalTag:     finalTag,
		MovingTag:    movingTag,
		CandidateTag: candidateTag,
		BaseRef:      flags.BaseRef,
		BaseInputID:  flags.BaseInputID,
	}, releaseTag, nil
}

// ledgerAddFinalTag resolves the final tag and its tag name from
// --final-tag / --final-tag-name plus the legacy image-name derivation.
// It returns the (possibly legacy-updated) flags for the later
// candidate-tag resolution.
func ledgerAddFinalTag(flags ledgerAddFlags, imageName string) (ledgerAddFlags, string, string, error) {
	finalTag := flags.FinalTag

	finalTagName := flags.FinalTagName
	if finalTag != "" && finalTagName != "" {
		return flags, "", "", fmt.Errorf("ledger add: --final-tag and --final-tag-name are mutually exclusive: %w", errs.ErrUsage)
	}

	flags, finalTag = ledgerAddApplyLegacyTags(flags, imageName, finalTag)

	if finalTag == "" && finalTagName != "" {
		if imageName == "" {
			return flags, "", "", fmt.Errorf("ledger add: --final-tag-name requires --image-name: %w", errs.ErrUsage)
		}

		finalTag = ledgerTagRef(imageName, finalTagName)
	}

	if finalTagName == "" {
		finalTagName = ledgerTagName(finalTag)
	}

	return flags, finalTag, finalTagName, nil
}

// ledgerAddApplyLegacyTags applies the legacy image-name mode: derive the
// final tag (and, when no candidate flag is in play, the candidate tag)
// from the image name and release tag.
func ledgerAddApplyLegacyTags(flags ledgerAddFlags, imageName, finalTag string) (ledgerAddFlags, string) {
	if !flags.LegacyImageNameMode || imageName == "" || flags.ReleaseTag == "" {
		return flags, finalTag
	}

	dFinal, dCandidate := imageledger.DeriveTags(imageName, flags.ReleaseTag)
	finalTag = firstNonEmpty(finalTag, dFinal)

	if flags.CandidateTag == "" && flags.CandidateTagName == "" && !flags.DeriveCandidateTag {
		flags.CandidateTag = dCandidate
	}

	return flags, finalTag
}

// ledgerAddMovingTag resolves the moving tag from --moving-tag /
// --moving-tag-name, enforcing their mutual exclusion.
func ledgerAddMovingTag(flags ledgerAddFlags, imageName string) (string, error) {
	movingTag := flags.MovingTag
	if movingTag != "" && flags.MovingTagName != "" {
		return "", fmt.Errorf("ledger add: --moving-tag and --moving-tag-name are mutually exclusive: %w", errs.ErrUsage)
	}

	if movingTag == "" && flags.MovingTagName != "" {
		if imageName == "" {
			return "", fmt.Errorf("ledger add: --moving-tag-name requires --image-name: %w", errs.ErrUsage)
		}

		movingTag = ledgerTagRef(imageName, flags.MovingTagName)
	}

	return movingTag, nil
}

// ledgerAddValidateCandidateFlags enforces the mutual exclusions between
// --candidate-tag, --candidate-tag-name, and --derive-candidate-tag.
func ledgerAddValidateCandidateFlags(flags ledgerAddFlags) error {
	if flags.CandidateTag != "" && flags.CandidateTagName != "" {
		return fmt.Errorf("ledger add: --candidate-tag and --candidate-tag-name are mutually exclusive: %w", errs.ErrUsage)
	}

	if flags.DeriveCandidateTag && (flags.CandidateTag != "" || flags.CandidateTagName != "") {
		return fmt.Errorf("ledger add: --derive-candidate-tag is mutually exclusive with explicit candidate tag flags: %w", errs.ErrUsage)
	}

	return nil
}

// ledgerAddCandidateTag resolves the candidate tag from --candidate-tag /
// --candidate-tag-name / --derive-candidate-tag.
func ledgerAddCandidateTag(flags ledgerAddFlags, imageName, finalTagName string) (string, error) {
	if err := ledgerAddValidateCandidateFlags(flags); err != nil {
		return "", err
	}

	candidateTag := flags.CandidateTag
	if candidateTag == "" && flags.CandidateTagName != "" {
		if imageName == "" {
			return "", fmt.Errorf("ledger add: --candidate-tag-name requires --image-name: %w", errs.ErrUsage)
		}

		candidateTag = ledgerTagRef(imageName, flags.CandidateTagName)
	}

	if candidateTag == "" && flags.DeriveCandidateTag {
		if imageName == "" {
			return "", fmt.Errorf("ledger add: --derive-candidate-tag requires --image-name: %w", errs.ErrUsage)
		}

		if finalTagName == "" {
			return "", fmt.Errorf("ledger add: --derive-candidate-tag requires a final tag name: %w", errs.ErrUsage)
		}

		candidateTag = ledgerTagRef(imageName, imageledger.StagingTagPrefix+finalTagName)
	}

	return candidateTag, nil
}

// ledgerAddSBOM resolves the SBOM path: --sbom wins, else --default-sbom
// derives the conventional dist path from the flavor or kind.
func ledgerAddSBOM(flags ledgerAddFlags) (string, error) {
	sbom := flags.SBOM
	if sbom == "" && flags.DefaultSBOM {
		suffix := firstNonEmpty(flags.Flavor, flags.Kind)
		if suffix == "" {
			return "", fmt.Errorf("ledger add: --default-sbom requires --flavor or --kind: %w", errs.ErrUsage)
		}

		sbom = "dist/image-sbom-" + suffix + ".cyclonedx.json"
	}

	return sbom, nil
}

func ledgerAddImageName(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	imageName := strings.TrimPrefix(raw, "docker://")

	imageName = domaincontainer.StripTagOrDigest(imageName)
	if imageName == "" {
		return "", fmt.Errorf("ledger add: --image-name is empty after normalisation: %w", errs.ErrUsage)
	}

	return imageName, nil
}

func ledgerAddReleaseTag(explicit, finalTagName string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	matches := releaseTagFromImageTagRE.FindStringSubmatch(finalTagName)
	if len(matches) == 2 {
		return matches[1], nil
	}

	return "", fmt.Errorf("ledger add: --tag is required when final tag name is not scoped to vMAJOR.MINOR.PATCH: %s: %w", finalTagName, errs.ErrUsage)
}

func ledgerTagRef(imageName, tagName string) string {
	return imageName + ":" + tagName
}

func ledgerTagName(ref string) string {
	if colon := strings.LastIndex(ref, ":"); colon >= 0 {
		return ref[colon+1:]
	}

	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// captureEntryDigest resolves the entry's image digest from the registry
// — the authoritative single source (no hand-typed digest, no
// transcription gap) — and rewrites Digest + the digest-pinned Ref from
// it. It reads whichever tag the consumer just pushed: the candidate tag
// (staging flow) when set, else the final tag. Matches the design the
// migration plan recommends (§8) over a consumer-supplied digest.
func captureEntryDigest(ctx context.Context, resolver imageledger.DigestResolver, entry *imageledger.Entry) error {
	src := entry.DigestSource()
	if src == "" {
		return fmt.Errorf("ledger add --capture-digest needs a candidate or final tag to resolve: %w", errs.ErrUsage)
	}

	digest, err := resolver.ResolveDigest(ctx, src)
	if err != nil {
		return fmt.Errorf("ledger add: capture digest from %s: %w", src, err)
	}

	entry.PinDigest(digest)

	return nil
}

func ledgerValidateCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdValidate,
		Usage: "re-validate every entry in the ledger against the release tag (trust-boundary check)",
		Description: `EXAMPLE:
   reusable-ci container ledger validate --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			&cli.BoolFlag{Name: "non-empty", Usage: "fail when the ledger contains no entries"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.ValidateAll(entries, cmd.String(flagTag)); err != nil {
				return err
			}

			if cmd.Bool("non-empty") && len(entries) == 0 {
				return fmt.Errorf("imageledger: ledger must contain at least one entry: %w", errs.ErrValidation)
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) valid for release tag %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}

func ledgerVerifyDigestsCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-digests",
		Usage: "re-verify each entry's recorded digest against what the registry serves (candidate_tag → final_tag → digest ref); distinct from `validate`, which checks entries against the release tag offline",
		Description: `EXAMPLE:
   reusable-ci container ledger verify-digests --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.Verify(ctx, ociregistry.New(), entries, cmd.String(flagTag)); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) verified against the registry for %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}

func ledgerSignCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdSign,
		Usage: "sign and attest every release image recorded in the ledger",
		Description: `Signer-side release-image loop: validates the digest-first ledger,
resolves each image by candidate_tag or digest ref, cosign-signs the immutable
digest, generates a CycloneDX image SBOM with syft, attests that SBOM, enriches
the release SLSA predicate with per-image/base-lineage fields, and attests it.

This is the reusable-ci replacement for forgejo-ci's sign-promote-images.sh sign
step; promotion remains a separate ledger promote operation.`,
		Flags: append(
			[]cli.Flag{
				ledgerPathFlag(),
				releaseTagFlag(),
				&cli.StringFlag{Name: "provenance-predicate", Value: "dist/slsa-provenance.predicate.json", Sources: cli.EnvVars("SLSA_PROVENANCE_PREDICATE"), Usage: "base SLSA provenance predicate JSON enriched per image before attestation"},
				&cli.StringFlag{Name: flagProvenanceEnvelope, Sources: cli.EnvVars("SLSA_PROVENANCE_ENVELOPE"), Usage: "in-toto statement JSON; its .predicate is enriched per image before attestation"},
				&cli.BoolFlag{Name: flagRecursive, Sources: cli.EnvVars("LEDGER_SIGN_RECURSIVE"), Usage: "pass --recursive to cosign sign/attest for manifest-list children (default false to match forgejo-ci signer behavior)"},
				&cli.StringFlag{Name: flagExpectedImageRepository, Sources: cli.EnvVars("LEDGER_SIGN_EXPECTED_IMAGE_REPOSITORY", "LEDGER_EXPECTED_IMAGE_REPOSITORY"), Usage: "optional exact image repository allowed for ref, final_tag, moving_tag, and candidate_tag (for forge-specific signer boundaries)"},
				&cli.StringFlag{Name: flagExpectedBaseRepository, Sources: cli.EnvVars("LEDGER_SIGN_EXPECTED_BASE_REPOSITORY"), Usage: "optional exact base image repository allowed for base_ref"},
				&cli.StringFlag{Name: flagSBOMPathPattern, Sources: cli.EnvVars("LEDGER_SIGN_SBOM_PATH_PATTERN"), Usage: "optional regular expression every ledger SBOM path must match"},
			},
			signflags.Cosign(signflags.CosignOpts{MethodNote: cosignMethodNoteGPG})...,
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			method, err := domainrelease.ParseSignMethod(cmd.String("method"))
			if err != nil {
				return err
			}

			predicatePath := cmd.String("provenance-predicate")

			predicateEnvelopePath := cmd.String(flagProvenanceEnvelope)
			if predicateEnvelopePath != "" {
				predicatePath = ""
			}

			return runLedgerSign(ctx,
				cosign.New(),
				ociregistry.New(),
				appcontainer.SignLedgerImagesInput{
					Entries:                 entries,
					ReleaseTag:              cmd.String(flagTag),
					PredicatePath:           predicatePath,
					PredicateEnvelopePath:   predicateEnvelopePath,
					Method:                  method,
					Recursive:               cmd.Bool(flagRecursive),
					KeyRef:                  cmd.String("key"),
					OIDCIssuer:              cmd.String("oidc-issuer"),
					ExpectedImageRepository: cmd.String(flagExpectedImageRepository),
					ExpectedBaseRepository:  cmd.String(flagExpectedBaseRepository),
					SBOMPathPattern:         cmd.String(flagSBOMPathPattern),
				})
		},
	}
}

// runLedgerSign is the shared sign body for `ledger sign` and
// `release-images sign`: hand the validated ledger entries to the
// app-layer signer with the package's syft evidence adapter and stderr
// streams. The caller picks the cosign environment (ambient vs
// signer-isolated) and the digest resolver (ambient vs auth-file).
func runLedgerSign(ctx context.Context, signer *cosign.Adapter, resolver *ociregistry.Adapter, in appcontainer.SignLedgerImagesInput) error {
	return appcontainer.SignLedgerImages(ctx,
		signer,
		&syft.Adapter{UnsetEnv: signerSecretEnv()},
		resolver,
		os.Stderr,
		os.Stderr,
		in)
}

func signerSecretEnv() []string {
	return []string{
		"COSIGN_KEY",
		"COSIGN_PASSWORD",
		"FORGEJO_TOKEN",
		"GPG_SIGNING_FINGERPRINT",
		"GPG_SIGNING_KEY",
		"GPG_SIGNING_PASSWORD",
		"MISE_FORGEJO_TOKEN",
		"MISE_GITHUB_TOKEN",
		"REUSABLE_CI_PROVIDER_TOKEN",
		"REGISTRY_PASSWORD",
		"REGISTRY_TOKEN",
		envRegistryUser,
	}
}

func ledgerPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdPromote,
		Usage: "promote each entry's candidate image to the stage's moving pointer (<base>:<stage>) on the same digest, verifying after each copy; the immutable :<version> tag is build-only",
		Description: `EXAMPLES:
   # Promote to the :release pointer (release scope requires --tag)
   reusable-ci container ledger promote --ledger release-images.json --tag v1.2.3 --stage release

   # Preview a staging promotion without touching the registry
   reusable-ci container ledger promote --ledger release-images.json --stage staging --dry-run`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			stageFlag(),
			stageRepoFlag(),
			releaseTagsFromLedgerFlag(),
			digestRefFallbackFlag(),
			expectedImageRepositoryFlag(),
			promotionJournalFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			entries, err := ledgerEntriesFromCmd(cmd)
			if err != nil {
				return err
			}

			stage := imageledger.Stage{
				Name:                   cmd.String("stage"),
				TargetRepo:             cmd.String("stage-repo"),
				UseEntryReleaseTags:    cmd.Bool("release-tags-from-ledger"),
				AllowDigestRefFallback: cmd.Bool("allow-digest-ref-fallback"),
			}

			// A cross-registry destination (--stage-repo) copies the
			// signature via cosign; same-repo promotions never consult it.
			var realSigCopier imageledger.SignatureCopier
			if stage.TargetRepo != "" {
				realSigCopier = cosignSigCopier{cosign: cosign.New(), out: os.Stderr}
			}

			reg, sigCopier := promotionRegistries(ociregistry.New(), dryrun.Enabled(cmd), realSigCopier)

			return runLedgerPromotion(ctx, promotionRun{
				reg:            reg,
				sigCopier:      sigCopier,
				entries:        entries,
				releaseTag:     cmd.String(flagTag),
				stage:          stage,
				journal:        cmd.String("journal"),
				journalDirPerm: 0o755, //nolint:mnd // dist-style state dir; 0755 is conventional.
				errPrefix:      "ledger",
				done:           fmt.Sprintf("ledger: promoted %d entr(y/ies) to stage %q", len(entries), stage.Name),
				out:            os.Stderr,
			})
		},
	}
}

// promotionRun carries the shared promote body's inputs; `ledger
// promote` and `release-images promote` both build one and call
// runLedgerPromotion.
type promotionRun struct {
	reg            imageledger.Registry
	sigCopier      imageledger.SignatureCopier
	entries        []imageledger.Entry
	releaseTag     string
	stage          imageledger.Stage
	journal        string      // empty: skip the journal write
	journalDirPerm os.FileMode // mode for a created journal parent dir
	errPrefix      string      // error/message prefix: "ledger" or "release images"
	done           string      // success line printed after promotion
	out            io.Writer   // success-line sink (stderr)
}

// promotionRegistries builds the promote registry pair: dry-run
// simulates copies in memory and previews them (the simulated registry
// doubles as the signature copier); a real run narrates each copy via
// auditCopier (audit trail) and uses the caller's signature copier
// (cosign for cross-registry promotion, nil when unused).
func promotionRegistries(resolver imageledger.Registry, dryRun bool, realSigCopier imageledger.SignatureCopier) (imageledger.Registry, imageledger.SignatureCopier) {
	if dryRun {
		sim := newDryRunRegistry(resolver, os.Stderr)

		return sim, sim
	}

	return auditCopier{Registry: resolver, out: os.Stderr}, realSigCopier
}

// runLedgerPromotion is the shared promote body: journal the promotion
// plan first (the rollback source of truth), promote every entry to the
// stage, then report the caller's success line.
func runLedgerPromotion(ctx context.Context, run promotionRun) error {
	if err := writePromotionJournal(ctx, run); err != nil {
		return err
	}

	if err := imageledger.PromoteToStage(ctx, run.reg, run.sigCopier, run.entries, run.releaseTag, run.stage); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(run.out, run.done)

	return nil
}

// writePromotionJournal plans the promotion and writes the journal file
// when a journal path is set; with no journal path it is a no-op.
func writePromotionJournal(ctx context.Context, run promotionRun) error {
	if run.journal == "" {
		return nil
	}

	records, err := imageledger.PlanPromotionJournal(ctx, run.reg, run.entries, run.releaseTag, run.stage)
	if err != nil {
		return err
	}

	body, err := imageledger.MarshalPromotionJournal(records)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(run.journal), run.journalDirPerm); err != nil {
		return fmt.Errorf("%s: create dir for promotion journal %s: %w", run.errPrefix, run.journal, err)
	}

	if err := cliio.WriteFile(run.journal, body, 0o600); err != nil {
		return fmt.Errorf("%s: write promotion journal %s: %w", run.errPrefix, run.journal, err)
	}

	return nil
}

func ledgerCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCleanup,
		Usage: "delete each entry's staging candidate tag after verifying the promoted final tag (leaves candidates in place if unverified)",
		Description: `EXAMPLE:
   reusable-ci container ledger cleanup --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			expectedImageRepositoryFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				entries, err := ledgerEntriesFromCmd(cmd)
				if err != nil {
					return err
				}

				reg, err := cleanupReg(d, dryrun.Enabled(cmd))
				if err != nil {
					return err
				}

				return runLedgerCleanup(ctx, reg, entries, cmd.String(flagTag), "ledger")
			})
		},
	}
}

func ledgerRollbackCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRollback,
		Usage: "undo a stage's promotion: delete that stage's pointer tag(s) (e.g. :dev/:staging/:release) that still serve the entry's digest; the immutable :<version> tag is never touched",
		Description: `EXAMPLE:
   reusable-ci container ledger rollback --ledger release-images.json --tag v1.2.3 --stage release`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			stageFlag(),
			stageRepoFlag(),
			releaseTagsFromLedgerFlag(),
			expectedImageRepositoryFlag(),
			promotionJournalFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				if journal := cmd.String("journal"); journal != "" {
					return ledgerRollbackFromJournal(ctx, cmd, dep, journal)
				}

				return ledgerRollbackFromLedger(ctx, cmd, dep)
			})
		},
	}
}

// ledgerRollbackFromJournal undoes a promotion from its promotion journal:
// the journal records exactly which pointer tags the promotion created.
func ledgerRollbackFromJournal(ctx context.Context, cmd *cli.Command, dep *deps.Deps, journal string) error {
	records, err := loadPromotionJournal(journal, "ledger", cmd.String(flagExpectedImageRepository))
	if err != nil {
		return err
	}

	reg, err := promotionRollbackReg(dep, dryrun.Enabled(cmd))
	if err != nil {
		return err
	}

	return runPromotionJournalRollback(ctx, reg, records, cmd.String(flagTag), "ledger")
}

// ledgerRollbackFromLedger undoes a stage's promotion from the ledger
// itself when no promotion journal is available.
func ledgerRollbackFromLedger(ctx context.Context, cmd *cli.Command, dep *deps.Deps) error {
	entries, err := ledgerEntriesFromCmd(cmd)
	if err != nil {
		return err
	}

	reg, err := cleanupReg(dep, dryrun.Enabled(cmd))
	if err != nil {
		return err
	}

	stage := imageledger.Stage{
		Name:                cmd.String("stage"),
		TargetRepo:          cmd.String("stage-repo"),
		UseEntryReleaseTags: cmd.Bool("release-tags-from-ledger"),
	}
	if err := imageledger.Rollback(ctx, reg, entries, cmd.String(flagTag), stage); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "ledger: rolled back %d entr(y/ies) for stage %q\n", len(entries), stage.Name)

	return nil
}

func ledgerMergeCmd() *cli.Command {
	return &cli.Command{
		Name:      "merge",
		Usage:     "merge several per-container ledger files into one (drops exact duplicates); for promoting a multi-container release as a single unit",
		ArgsUsage: "<path>... (files, or directories searched for release-images.json)",
		Description: `EXAMPLE:
   reusable-ci container ledger merge dist/container-a dist/container-b --ledger release-images.json`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			docs, err := readLedgerDocs(cmd.Args().Slice())
			if err != nil {
				return err
			}

			out, err := imageledger.Merge(docs)
			if err != nil {
				return err
			}

			// A merge that yields no entries means no ledger was found where one
			// was expected (e.g. the build's best-effort ledger upload didn't
			// run). Promotion will be a safe no-op, but surface it as a
			// forge-aware annotation (::warning:: on GitHub; a plain Warning:
			// line elsewhere, incl. Forgejo) so it is visible rather than a
			// silently green run.
			if entries, perr := imageledger.Parse(out); perr == nil && len(entries) == 0 {
				deps.Annotator(cmd).Warningf("ledger merge: no entries found under %v — promotion will be a no-op; check the build's ledger upload", cmd.Args().Slice())
			}

			path := cmd.String(flagLedger)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // dist dir; 0755 is conventional.
				return fmt.Errorf("ledger: create dir for %s: %w", path, err)
			}

			if err := cliio.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // ledger is public release metadata, not a secret.
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: merged %d source(s) → %s\n", len(docs), path)

			return nil
		},
	}
}

// readLedgerDocs reads ledger JSON from each path: a file is read directly, a
// directory is walked for files named release-images.json (the per-container
// artifacts the promotion job downloads). Missing inputs are an error so a
// silent empty merge can't mask a broken hand-off.
func readLedgerDocs(paths []string) ([][]byte, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("ledger merge: at least one input path is required: %w", errs.ErrMissingInput)
	}

	var docs [][]byte

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// A missing input degrades to "no ledger here" rather than a hard
				// failure: when the build's best-effort ledger upload didn't run,
				// the promotion job finds no artifacts and must no-op (the image
				// still ships with its build tags) instead of reddening a release
				// whose signed artifacts already published.
				_, _ = fmt.Fprintf(os.Stderr, "ledger merge: %s not found, skipping\n", path)

				continue
			}

			return nil, fmt.Errorf("ledger merge: %s: %w", path, err)
		}

		if info.IsDir() {
			found, walkErr := readLedgerDir(path)
			if walkErr != nil {
				return nil, walkErr
			}

			docs = append(docs, found...)

			continue
		}

		data, readErr := cliio.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("ledger merge: read %s: %w", path, readErr)
		}

		docs = append(docs, data)
	}

	return docs, nil
}

// readLedgerDir walks dir and reads every file named defaultLedgerBasename —
// the per-container ledger artifacts the promotion job downloads into one
// directory tree.
func readLedgerDir(dir string) ([][]byte, error) {
	var docs [][]byte

	walkErr := filepath.WalkDir(dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || entry.Name() != defaultLedgerBasename {
			return nil
		}

		data, readErr := os.ReadFile(filePath) //nolint:gosec // operator-supplied ledger directory.
		if readErr != nil {
			return fmt.Errorf("read %s: %w", filePath, readErr)
		}

		docs = append(docs, data)

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("ledger merge: walk %s: %w", dir, walkErr)
	}

	return docs, nil
}
