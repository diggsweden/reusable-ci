// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
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
