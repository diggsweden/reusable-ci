// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// stubResolver maps refs → digests for the dry-run decorator test.
type stubResolver map[string]string

func (s stubResolver) ResolveDigest(_ context.Context, ref string) (string, error) {
	d, ok := s[ref]
	if !ok {
		return "", errors.New("not found") //nolint:err113 // test mock error
	}

	return d, nil
}

func TestDryRunRegistry_SimulatesCopyAndLogsWithoutMutating(t *testing.T) {
	t.Parallel()

	const dig = "sha256:abc"

	var buf bytes.Buffer

	d := newDryRunRegistry(stubResolver{"reg/repo:staging": dig}, &buf)
	ctx := context.Background()

	// A copy is simulated, not performed: it logs and makes the dest
	// resolve to the source digest so the domain's post-copy verify passes.
	if err := d.CopyTag(ctx, "reg/repo:staging", "reg/repo:v1"); err != nil {
		t.Fatal(err)
	}

	if got, _ := d.ResolveDigest(ctx, "reg/repo:v1"); got != dig {
		t.Errorf("simulated dest digest = %q, want %q", got, dig)
	}

	if err := d.DeleteTag(ctx, "reg/repo:staging"); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "would promote reg/repo:staging -> reg/repo:v1") {
		t.Errorf("missing promote preview: %q", out)
	}

	if !strings.Contains(out, "would delete reg/repo:staging") {
		t.Errorf("missing delete preview: %q", out)
	}
}

// recordingRegistry is a CleanupRegistry+Registry that records the
// mutations it was asked to perform, so the audit decorators can be
// tested for both delegation and narration.
type recordingRegistry struct {
	stubResolver

	copied  []string
	deleted []string
}

func (r *recordingRegistry) CopyTag(_ context.Context, source, dest string) error {
	r.copied = append(r.copied, source+"->"+dest)

	return nil
}

func (r *recordingRegistry) DeleteTag(_ context.Context, ref string) error {
	r.deleted = append(r.deleted, ref)

	return nil
}

// TestAuditDecorators_NarrateAndDelegate is the defense-in-depth audit
// check: a real (non-dry-run) copy/delete must both reach the underlying
// registry AND leave a per-tag line in the log, so destructive mutations
// are as observable as their --dry-run preview.
func TestAuditDecorators_NarrateAndDelegate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rec := &recordingRegistry{stubResolver: stubResolver{}}

	var buf bytes.Buffer

	if err := (auditCopier{Registry: rec, out: &buf}).CopyTag(ctx, "reg/repo:staging", "reg/repo:v1"); err != nil {
		t.Fatal(err)
	}

	if err := (auditDeleter{CleanupRegistry: rec, out: &buf}).DeleteTag(ctx, "reg/repo:staging"); err != nil {
		t.Fatal(err)
	}

	// Delegated to the real registry.
	if len(rec.copied) != 1 || rec.copied[0] != "reg/repo:staging->reg/repo:v1" {
		t.Errorf("copy not delegated: %v", rec.copied)
	}

	if len(rec.deleted) != 1 || rec.deleted[0] != "reg/repo:staging" {
		t.Errorf("delete not delegated: %v", rec.deleted)
	}

	// And narrated to the audit log.
	out := buf.String()
	if !strings.Contains(out, "promoting reg/repo:staging -> reg/repo:v1") {
		t.Errorf("missing promote audit line: %q", out)
	}

	if !strings.Contains(out, "deleting reg/repo:staging") {
		t.Errorf("missing delete audit line: %q", out)
	}
}

// TestLedgerMutatingVerbsHaveDryRun is the guardrail: every destructive
// registry verb must support --dry-run so operators can preview before
// mutating. Grow `required` as --dry-run coverage expands to other verbs.
func TestLedgerMutatingVerbsHaveDryRun(t *testing.T) {
	t.Parallel()

	required := map[string]bool{"promote": true, "cleanup": true, "rollback": true}

	for _, sub := range ledgerGroup().Commands {
		if !required[sub.Name] {
			continue
		}

		if !hasFlag(sub, "dry-run") {
			t.Errorf("ledger %q is a mutating verb and must expose --dry-run", sub.Name)
		}

		delete(required, sub.Name)
	}

	for name := range required {
		t.Errorf("expected ledger subcommand %q not found", name)
	}
}

func TestLedgerPromoteRollbackExposePromotionJournal(t *testing.T) {
	t.Parallel()

	for _, sub := range ledgerGroup().Commands {
		switch sub.Name {
		case "promote", "rollback":
			if !hasFlag(sub, "journal") {
				t.Errorf("ledger %q must expose --journal", sub.Name)
			}
		}
	}
}

func TestLedgerPromoteExposesDigestRefFallback(t *testing.T) {
	t.Parallel()

	for _, sub := range ledgerGroup().Commands {
		if sub.Name != "promote" {
			continue
		}

		for _, flag := range []string{"allow-digest-ref-fallback", "expected-image-repository"} {
			if !hasFlag(sub, flag) {
				t.Errorf("ledger promote must expose --%s", flag)
			}
		}

		return
	}

	t.Fatal("ledger promote command not found")
}

func TestLedgerRollbackAndCleanupExposeExpectedRepository(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}

	for _, sub := range ledgerGroup().Commands {
		switch sub.Name {
		case "rollback", "cleanup":
			found[sub.Name] = true
			if !hasFlag(sub, "expected-image-repository") {
				t.Errorf("ledger %s must expose --expected-image-repository", sub.Name)
			}
		}
	}

	for _, name := range []string{"rollback", "cleanup"} {
		if !found[name] {
			t.Fatalf("ledger %s command not found", name)
		}
	}
}

func TestLedgerSignCommandExposesSignerFlags(t *testing.T) {
	t.Parallel()

	for _, sub := range ledgerGroup().Commands {
		if sub.Name != "sign" {
			continue
		}

		for _, flag := range []string{"ledger", "tag", "provenance-predicate", "provenance-envelope", "method", "key", "recursive", "expected-image-repository", "expected-base-repository", "sbom-path-pattern"} {
			if !hasFlag(sub, flag) {
				t.Errorf("ledger sign missing --%s", flag)
			}
		}

		return
	}

	t.Fatal("ledger sign command not found")
}

func TestLedgerValidateCommandExposesNonEmpty(t *testing.T) {
	t.Parallel()

	for _, sub := range ledgerGroup().Commands {
		if sub.Name != "validate" {
			continue
		}

		if !hasFlag(sub, "non-empty") {
			t.Error("ledger validate missing --non-empty")
		}

		return
	}

	t.Fatal("ledger validate command not found")
}

func TestCaptureEntryDigest(t *testing.T) {
	t.Parallel()

	const dig = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// Staging flow: resolve from the candidate tag, rewrite Ref + Digest.
	e := imageledger.Entry{
		CandidateTag: "codeberg.org/itiquette/repo:staging-v1.2.3",
		FinalTag:     "codeberg.org/itiquette/repo:v1.2.3",
	}
	if err := captureEntryDigest(context.Background(), stubResolver{e.CandidateTag: dig}, &e); err != nil {
		t.Fatal(err)
	}

	if e.Digest != dig {
		t.Errorf("Digest = %q", e.Digest)
	}

	if e.Ref != "codeberg.org/itiquette/repo@"+dig {
		t.Errorf("Ref = %q (should be candidate's registry path + @digest)", e.Ref)
	}

	// Direct push: no candidate → resolve from the final tag.
	e2 := imageledger.Entry{FinalTag: "codeberg.org/itiquette/repo:v1.2.3"}
	if err := captureEntryDigest(context.Background(), stubResolver{e2.FinalTag: dig}, &e2); err != nil {
		t.Fatal(err)
	}

	if e2.Ref != "codeberg.org/itiquette/repo@"+dig {
		t.Errorf("direct-push Ref = %q", e2.Ref)
	}

	// No tag to resolve → usage error.
	if err := captureEntryDigest(context.Background(), stubResolver{}, &imageledger.Entry{}); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("no tag should be a usage error, got %v", err)
	}
}

func TestLedgerAddEntryFromFlags_ShapesForgejoReleaseImageRecord(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	entry, releaseTag, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		ReleaseTag:       "v1.2.3",
		Role:             "flavor",
		ImageKind:        string(imageledger.ImageKindRelease),
		Flavor:           "rust",
		ImageName:        "docker://codeberg.org/itiquette/nanolinter:ignored",
		Digest:           dig,
		FinalTagName:     "v1.2.3-rust",
		CandidateTagName: "staging-v1.2.3-rust",
		MovingTagName:    "rust",
		BaseRef:          "codeberg.org/itiquette/nanolinter-base@" + dig,
		BaseInputID:      "base-input",
		DefaultSBOM:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if releaseTag != "v1.2.3" {
		t.Fatalf("releaseTag = %q", releaseTag)
	}

	want := imageledger.Entry{
		Role:         "flavor",
		ImageKind:    imageledger.ImageKindRelease,
		Flavor:       "rust",
		Ref:          "codeberg.org/itiquette/nanolinter@" + dig,
		Digest:       dig,
		SBOM:         "dist/image-sbom-rust.cyclonedx.json",
		FinalTag:     "codeberg.org/itiquette/nanolinter:v1.2.3-rust",
		MovingTag:    "codeberg.org/itiquette/nanolinter:rust",
		CandidateTag: "codeberg.org/itiquette/nanolinter:staging-v1.2.3-rust",
		BaseRef:      "codeberg.org/itiquette/nanolinter-base@" + dig,
		BaseInputID:  "base-input",
	}
	if !reflect.DeepEqual(entry, want) {
		t.Fatalf("entry = %#v\nwant  %#v", entry, want)
	}
}

func TestLedgerAddEntryFromFlags_DirectPushDoesNotInventCandidate(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	entry, releaseTag, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		Role:         "distroless",
		ImageName:    "codeberg.org/itiquette/gommitlint",
		Digest:       dig,
		FinalTagName: "v1.2.3",
		SBOM:         "dist/image-sbom.cyclonedx.json",
	})
	if err != nil {
		t.Fatal(err)
	}

	if releaseTag != "v1.2.3" {
		t.Fatalf("releaseTag = %q", releaseTag)
	}

	if entry.CandidateTag != "" {
		t.Fatalf("CandidateTag = %q, want empty", entry.CandidateTag)
	}

	if entry.Ref != "codeberg.org/itiquette/gommitlint@"+dig {
		t.Fatalf("Ref = %q", entry.Ref)
	}
}

func TestLedgerAddEntryFromFlags_CanDeriveCandidateWhenRequested(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	entry, _, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		ReleaseTag:         "v1.2.3",
		ImageName:          "codeberg.org/itiquette/nanolinter",
		Digest:             dig,
		FinalTagName:       "v1.2.3-alpine",
		DeriveCandidateTag: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if entry.CandidateTag != "codeberg.org/itiquette/nanolinter:staging-v1.2.3-alpine" {
		t.Fatalf("CandidateTag = %q", entry.CandidateTag)
	}
}

func TestLedgerAddEntryFromFlags_KeepsLegacyImageNameMode(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	entry, releaseTag, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		ReleaseTag:          "v1.2.3",
		ImageName:           "codeberg.org/owner/repo",
		Digest:              dig,
		LegacyImageNameMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if releaseTag != "v1.2.3" {
		t.Fatalf("releaseTag = %q", releaseTag)
	}

	if entry.FinalTag != "codeberg.org/owner/repo:v1.2.3" {
		t.Fatalf("FinalTag = %q", entry.FinalTag)
	}

	if entry.CandidateTag != "codeberg.org/owner/repo:staging-v1.2.3" {
		t.Fatalf("CandidateTag = %q", entry.CandidateTag)
	}
}

func TestLedgerAddEntryFromFlags_RecordsSBOMPinAndProvenanceExtras(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	pin := strings.Repeat("a", 64)

	entry, _, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		Role:           "distroless",
		ImageName:      "codeberg.org/itiquette/gommitlint",
		Digest:         dig,
		FinalTagName:   "v1.2.3",
		SBOM:           "dist/image-sbom.cyclonedx.json",
		SBOMSHA256:     pin,
		ProvenanceJSON: `{"base_input_set":"abc123","build_group":"core"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if entry.SBOMSHA256 != pin {
		t.Errorf("SBOMSHA256 = %q, want %q", entry.SBOMSHA256, pin)
	}

	wantExtras := map[string]any{"base_input_set": "abc123", "build_group": "core"}
	if !reflect.DeepEqual(entry.Provenance, wantExtras) {
		t.Errorf("Provenance = %#v, want %#v", entry.Provenance, wantExtras)
	}

	// The domain accepts the shaped entry, so `ledger add` records it.
	if _, _, err := imageledger.Append(nil, entry, "v1.2.3"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// A malformed pin is refused by the domain BEFORE any write: the
	// validation runs inside Append, ahead of the ledger update.
	bad := entry
	bad.SBOMSHA256 = "not-hex"

	if _, _, err := imageledger.Append(nil, bad, "v1.2.3"); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("bad pin err = %v, want ErrValidation", err)
	}

	// A pin without an SBOM path to verify is likewise refused.
	orphan := entry
	orphan.SBOM = ""

	if _, _, err := imageledger.Append(nil, orphan, "v1.2.3"); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("pin without sbom err = %v, want ErrValidation", err)
	}
}

func TestLedgerAddEntryFromFlags_RejectsMalformedProvenanceJSON(t *testing.T) {
	t.Parallel()

	const dig = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	for name, raw := range map[string]string{
		"invalid":   `{not json`,
		"array":     `[1,2]`,
		"string":    `"x"`,
		"null":      `null`,
		"truncated": `{"a":`,
	} {
		_, _, err := ledgerAddEntryFromFlags(ledgerAddFlags{
			Role:           "distroless",
			ImageName:      "codeberg.org/itiquette/gommitlint",
			Digest:         dig,
			FinalTagName:   "v1.2.3",
			ProvenanceJSON: raw,
		})
		if !errors.Is(err, errs.ErrMalformedInput) {
			t.Errorf("%s: err = %v, want ErrMalformedInput", name, err)
		}
	}
}

func TestLedgerAddCommandExposesSBOMPinAndProvenanceFlags(t *testing.T) {
	t.Parallel()

	for _, sub := range ledgerGroup().Commands {
		if sub.Name != "add" {
			continue
		}

		for _, flag := range []string{"sbom-sha256", "provenance-json"} {
			if !hasFlag(sub, flag) {
				t.Errorf("ledger add missing --%s", flag)
			}
		}

		return
	}

	t.Fatal("ledger add command not found")
}

func TestLedgerAddEntryFromFlags_RejectsAmbiguousCandidateInputs(t *testing.T) {
	t.Parallel()

	_, _, err := ledgerAddEntryFromFlags(ledgerAddFlags{
		ReleaseTag:         "v1.2.3",
		ImageName:          "codeberg.org/owner/repo",
		FinalTagName:       "v1.2.3",
		DeriveCandidateTag: true,
		CandidateTagName:   "staging-v1.2.3",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want usage", err)
	}
}

func hasFlag(cmd *cli.Command, name string) bool {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return true
			}
		}
	}

	return false
}
