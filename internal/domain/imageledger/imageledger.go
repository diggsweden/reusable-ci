// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package imageledger models the digest-first release-image ledger: the
// typed hand-off between the build stage (which pushes images and
// records entries) and the sign/publish stage (which signs, attests, and
// promotes exactly those digests). It is forge-agnostic — entries are
// pure OCI/registry concepts (digests, refs, tags), so the same ledger
// serves GitHub (ghcr.io) and Forgejo (codeberg.org) alike.
//
// The validation here is first-line enforcement at record time; the
// signer re-validates the same rules at the trust
// boundary, so a build job cannot smuggle an out-of-scope tag past
// signing.
package imageledger

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// Entry is one image in the ledger.
//
// The promotion mechanism needs only Ref, Digest, and FinalTag (plus the
// optional candidate/moving tags) — those are required. Kind and SBOM are
// release-manifest metadata: optional, validated for format only when
// present. This keeps a promotion entry minimal while still letting a full
// release record carry the audit fields.
type Entry struct {
	// Kind is an optional image-role label (e.g. distroless, alpine).
	Kind         string `json:"kind,omitempty"`
	Flavor       string `json:"flavor,omitempty"`
	Ref          string `json:"ref"`
	Digest       string `json:"digest"`
	// SBOM is an optional path to the image's CycloneDX SBOM.
	SBOM         string `json:"sbom,omitempty"`
	FinalTag     string `json:"final_tag"`
	CandidateTag string `json:"candidate_tag,omitempty"`
	BaseRef      string `json:"base_ref,omitempty"`
	BaseInputID  string `json:"base_input_id,omitempty"`
}

// StagingTagPrefix is the candidate-tag prefix the trust boundary enforces:
// Entry.Validate requires a candidate of staging-<releaseTag>. Exposed so the
// build side derives exactly the name the validator checks — one source for the
// convention, shared by every forge that records into the ledger.
const StagingTagPrefix = "staging-"

// DeriveTags returns the final and candidate tag refs for an image at a release
// tag, matching Entry.Validate's scoping: final (<image>:<tag>) first, then
// candidate (<image>:staging-<tag>). It lets a build job pass an image name +
// release tag instead of hand-assembling both refs (and re-encoding the
// staging- prefix) in workflow YAML, keeping the convention in Go.
func DeriveTags(imageName, releaseTag string) (string, string) {
	return imageName + ":" + releaseTag, imageName + ":" + StagingTagPrefix + releaseTag
}

// Validation regexes for the OCI/registry refs the ledger records.
//
//nolint:gochecknoglobals // compiled regex table — read-only.
var (
	digestRE   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	imageRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+(:[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?@sha256:[0-9a-f]{64}$`)
	tagRefRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+:[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	// sbomRE accepts any relative CycloneDX path, matching the filenames
	// the sbom package emits (domain/sbom/filenames.go), rather than a
	// single fixed name. Anchored to a leaf-relative path (no leading '/').
	sbomRE = regexp.MustCompile(`^[A-Za-z0-9._-][A-Za-z0-9._/-]*\.cyclonedx\.json$`)
)

// tagName returns the tag portion of a registry ref (everything after
// the final ':').
func tagName(ref string) string {
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		return ref[i+1:]
	}

	return ref
}

// stripTag removes a trailing ":tag" from a registry ref, keeping the
// registry/path. A ':' that precedes the final '/' (a host:port) is left
// alone.
func stripTag(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon]
	}

	return ref
}

// repoPathAfterHost is a ref's repository path with the registry host (the
// first path segment) removed: ghcr.io/org/team/app:v1 → "org/team/app". Used
// to rehome an image under a cross-registry target prefix while PRESERVING its
// full source path, the registry-replication (skopeo-sync) idiom. Because two
// distinct source images necessarily have distinct paths, the rehomed
// destinations are distinct by construction — collision-free without relying on
// any external naming invariant.
func repoPathAfterHost(ref string) string {
	repo := stripTag(ref)
	if slash := strings.Index(repo, "/"); slash >= 0 {
		return repo[slash+1:]
	}

	return repo
}

// DigestSource is the tag whose digest pins this entry: the candidate
// (staging) tag when present, otherwise the final tag. Empty when neither
// is set.
func (e Entry) DigestSource() string {
	if e.CandidateTag != "" {
		return e.CandidateTag
	}

	return e.FinalTag
}

// PinDigest records the resolved digest on the entry and derives its
// canonical digest-pinned Ref (<registry>/<path>@<digest>) from the
// digest source. This keeps the "what an Entry.Ref is" invariant inside
// the package that owns the schema, rather than letting callers assemble
// the ref by hand.
func (e *Entry) PinDigest(digest string) {
	e.Digest = digest
	e.Ref = stripTag(e.DigestSource()) + "@" + digest
}

// Validate enforces the full trust-boundary rules for one entry against
// the release tag: the stage-agnostic format checks plus final_tag scoped
// to the release tag (releaseTag or releaseTag-<suffix>) and — when
// present — a candidate_tag scoped to staging-<releaseTag>. This is the
// terminal (release) check; ValidateForStage scopes it to a stage.
func (e Entry) Validate(releaseTag string) error {
	if releaseTag == "" {
		return fmt.Errorf("imageledger: release tag is required for validation: %w", errs.ErrUsage)
	}

	if err := e.validateFormat(); err != nil {
		return err
	}

	finalName := tagName(e.FinalTag)
	if finalName != releaseTag && !strings.HasPrefix(finalName, releaseTag+"-") {
		return fmt.Errorf("imageledger: final_tag %q must be scoped to release tag %q: %w", e.FinalTag, releaseTag, errs.ErrValidation)
	}

	// candidate_tag must be the exact staging counterpart of final_tag:
	// its tag is "staging-<release><final-suffix>". The exact match (not a
	// "staging-<release>*" prefix) ties each candidate to one final tag, so
	// a release promotes precisely the image that was staged for it.
	if e.CandidateTag != "" {
		want := StagingTagPrefix + releaseTag + strings.TrimPrefix(finalName, releaseTag)
		if tagName(e.CandidateTag) != want {
			return fmt.Errorf("imageledger: candidate_tag %q must have tag %q (staging counterpart of final_tag): %w", e.CandidateTag, want, errs.ErrValidation)
		}
	}

	return nil
}

// ValidateForStage scopes the trust-boundary check to a promotion stage.
// The release stage applies the full release-scoped rules (identical to
// Validate). A named, pre-release stage (dev, stage) applies only the
// stage-agnostic format/required checks, because no release tag exists yet
// — the entry is recorded at build time and its release scope is enforced
// only at the terminal release promotion.
func (e Entry) ValidateForStage(stage Stage, releaseTag string) error {
	if stage.IsRelease() {
		return e.Validate(releaseTag)
	}

	return e.validateFormat()
}

// validateFormat enforces the stage-agnostic trust-boundary invariants:
// the promotion-essential fields (ref, digest, final_tag) and their
// formats. Kind and SBOM are optional release-manifest metadata — SBOM is
// format-checked only when present. These hold at every promotion stage,
// so both Validate (release) and ValidateForStage (pre-release) build on
// them.
func (e Entry) validateFormat() error {
	for _, req := range []struct{ name, val string }{
		{"ref", e.Ref}, {"digest", e.Digest}, {"final_tag", e.FinalTag},
	} {
		if req.val == "" {
			return fmt.Errorf("imageledger: %s is required: %w", req.name, errs.ErrValidation)
		}
	}

	if !digestRE.MatchString(e.Digest) {
		return fmt.Errorf("imageledger: digest must be sha256:<64 hex>: %q: %w", e.Digest, errs.ErrValidation)
	}

	if !imageRefRE.MatchString(e.Ref) {
		return fmt.Errorf("imageledger: ref must be a registry path pinned by @sha256:<64 hex>: %q: %w", e.Ref, errs.ErrValidation)
	}

	if e.SBOM != "" && !sbomRE.MatchString(e.SBOM) {
		return fmt.Errorf("imageledger: sbom must be a relative CycloneDX path (*.cyclonedx.json): %q: %w", e.SBOM, errs.ErrValidation)
	}

	if !tagRefRE.MatchString(e.FinalTag) {
		return fmt.Errorf("imageledger: final_tag must be a registry path with a tag: %q: %w", e.FinalTag, errs.ErrValidation)
	}

	return nil
}

// ValidateAll validates every entry against the release tag, reporting
// the index of the first offender.
func ValidateAll(entries []Entry, releaseTag string) error {
	for i, e := range entries {
		if err := e.Validate(releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", i, err)
		}
	}

	return nil
}

// Parse decodes a ledger (a bare JSON array). Empty input is an empty
// ledger. A non-array document is rejected.
func Parse(data []byte) ([]Entry, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Unparseable JSON is malformed input (EX_DATAERR), not a failed
		// validation rule (EX_VALIDATION); Validate handles the latter.
		return nil, fmt.Errorf("imageledger: parse ledger (want a JSON array): %w", errs.ErrMalformedInput)
	}

	return entries, nil
}

// Marshal renders entries as an indented JSON array with a trailing
// newline. A nil/empty slice renders as "[]".
func Marshal(entries []Entry) ([]byte, error) {
	if entries == nil {
		entries = []Entry{}
	}

	body, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("imageledger: marshal ledger: %w", err)
	}

	return append(body, '\n'), nil
}

// Append validates entry against releaseTag and appends it to the ledger
// JSON, returning the updated document and whether the entry was newly
// added. Validation runs before the append, so a malformed entry never
// lands in the ledger.
//
// Append is idempotent: re-recording an entry that is already present
// (e.g. a retried build job re-running `ledger add` against a persisted
// dist/release-images.json) is a no-op — added is false and the document
// is returned unchanged — rather than a duplicate. A genuinely different
// image, even one sharing a final tag, is a distinct entry and is still
// appended (added true); that conflict is a validation concern, not an
// idempotency one. The added flag lets callers report the truthful
// outcome instead of always claiming an append.
func Append(ledgerJSON []byte, entry Entry, releaseTag string) ([]byte, bool, error) {
	if err := entry.Validate(releaseTag); err != nil {
		return nil, false, err
	}

	entries, err := Parse(ledgerJSON)
	if err != nil {
		return nil, false, err
	}

	// Entry is a flat all-string struct, so == is exact-value equality.
	var doc []byte

	for _, existing := range entries {
		if existing == entry {
			doc, err = Marshal(entries)

			return doc, false, err
		}
	}

	doc, err = Marshal(append(entries, entry))

	return doc, true, err
}
