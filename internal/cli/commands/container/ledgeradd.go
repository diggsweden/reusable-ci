// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

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
			&cli.StringFlag{Name: "image-kind", Value: string(imageledger.ImageKindRelease), Usage: "entry validation scope recorded as image_kind: release (default) or base"},
			&cli.StringFlag{Name: flagRef, Usage: "digest-pinned image ref (registry/path@sha256:<64 hex>)"},
			&cli.StringFlag{Name: flagDigest, Usage: "image digest (sha256:<64 hex>)"},
			&cli.StringFlag{Name: "sbom", Usage: "CycloneDX SBOM path (dist/image-sbom*.cyclonedx.json)"},
			&cli.StringFlag{Name: flagSBOMSHA256, Sources: cli.EnvVars("LEDGER_SBOM_SHA256"), Usage: "sha256 hex pin of the premade --sbom file; ledger sign verifies the pin and attests that exact document instead of generating a fresh one"},
			&cli.StringFlag{Name: "provenance-json", Sources: cli.EnvVars("LEDGER_PROVENANCE_JSON"), Usage: "JSON object of extra externalParameters recorded on the entry and merged into its enriched SLSA predicate at signing; engine-computed keys are reserved (a collision fails the signing run)"},
			&cli.StringFlag{Name: flagImageName, Usage: "image registry/path (no tag); with --tag, derives --final-tag=<image>:<tag> and --candidate-tag=<image>:staging-<tag> so callers don't hand-assemble both (explicit flags still win)"},
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
				ImageKind:           cmd.String("image-kind"),
				ImageName:           cmd.String(flagImageName),
				Ref:                 cmd.String(flagRef),
				Digest:              cmd.String(flagDigest),
				SBOM:                cmd.String("sbom"),
				SBOMSHA256:          cmd.String(flagSBOMSHA256),
				ProvenanceJSON:      cmd.String("provenance-json"),
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
	ImageKind           string
	ImageName           string
	Ref                 string
	Digest              string
	SBOM                string
	SBOMSHA256          string
	ProvenanceJSON      string
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

	sbom, provenanceExtras, err := ledgerAddEvidence(flags)
	if err != nil {
		return imageledger.Entry{}, "", err
	}

	ref := flags.Ref
	if ref == "" && imageName != "" && flags.Digest != "" {
		ref = imageName + "@" + flags.Digest
	}

	return imageledger.Entry{
		Kind:         flags.Kind,
		ImageKind:    imageledger.ImageKind(flags.ImageKind),
		Flavor:       flags.Flavor,
		Ref:          ref,
		Digest:       flags.Digest,
		SBOM:         sbom,
		SBOMSHA256:   flags.SBOMSHA256,
		Provenance:   provenanceExtras,
		FinalTag:     finalTag,
		MovingTag:    movingTag,
		CandidateTag: candidateTag,
		BaseRef:      flags.BaseRef,
		BaseInputID:  flags.BaseInputID,
	}, releaseTag, nil
}

// ledgerAddEvidence resolves the entry's evidence fields: the SBOM path
// (--sbom / --default-sbom) and the declared provenance extras
// (--provenance-json). The extras are parsed before any ledger write, so
// an invalid or non-object JSON document (malformed input) never reaches
// the file.
func ledgerAddEvidence(flags ledgerAddFlags) (string, map[string]any, error) {
	sbom, err := ledgerAddSBOM(flags)
	if err != nil {
		return "", nil, err
	}

	extras, err := provenance.ParseExternalParametersJSON(flags.ProvenanceJSON)
	if err != nil {
		return "", nil, fmt.Errorf("ledger add: --provenance-json: %w", err)
	}

	return sbom, extras, nil
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

func ledgerTagName(ref string) string {
	if colon := strings.LastIndex(ref, ":"); colon >= 0 {
		return ref[colon+1:]
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
