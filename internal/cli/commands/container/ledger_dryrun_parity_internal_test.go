// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"context"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// tagStore is an in-memory registry: tags map to digests, a copy moves the
// source's digest to the destination and a delete removes the tag, and every
// mutation is recorded in order.
type tagStore struct {
	tags       map[string]string
	operations []string
}

func (s *tagStore) ResolveDigest(_ context.Context, ref string) (string, error) {
	if digest, ok := s.tags[ref]; ok {
		return digest, nil
	}

	return "", errs.ErrMissingInput
}

func (s *tagStore) CopyTag(_ context.Context, source, dest string) error {
	s.tags[dest] = s.tags[source]
	s.operations = append(s.operations, "promote "+source+" -> "+dest+" ("+s.tags[source]+")")

	return nil
}

func (s *tagStore) DeleteTag(_ context.Context, ref string) error {
	delete(s.tags, ref)
	s.operations = append(s.operations, "delete "+ref)

	return nil
}

var dryRunLine = regexp.MustCompile(`^\[dry-run\] would (promote .+ -> .+ \(.+\)|delete .+)$`)

// previewOperations reads the operations a dry run narrated, one per line.
func previewOperations(t *testing.T, preview string) []string {
	t.Helper()

	var operations []string

	for line := range strings.SplitSeq(strings.TrimSpace(preview), "\n") {
		match := dryRunLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("dry-run line %q is not an operation preview", line)
		}

		operations = append(operations, match[1])
	}

	return operations
}

// TestLedgerDryRun_PreviewsExactlyTheOperationsTheWetRunPerforms runs promote
// and then cleanup for two images that share nothing but a repository, once
// through the dry-run registry and once for real against an in-memory
// registry. The preview names, for each operation, the source, the
// destination and the digest, and it names exactly the operations the real
// run then performs, second entry included, while the dry run changes nothing.
func TestLedgerDryRun_PreviewsExactlyTheOperationsTheWetRunPerforms(t *testing.T) {
	t.Parallel()

	const (
		rustDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		goDigest   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		repository = "codeberg.org/itiquette/app"
	)

	entries := []imageledger.Entry{
		{
			Ref: repository + "@" + rustDigest, Digest: rustDigest, Flavor: "rust",
			CandidateTag: repository + ":staging-v1.2.3-rust", FinalTag: repository + ":v1.2.3-rust", MovingTag: repository + ":rust",
		},
		{
			Ref: repository + "@" + goDigest, Digest: goDigest, Flavor: "go",
			CandidateTag: repository + ":staging-v1.2.3-go", FinalTag: repository + ":v1.2.3-go", MovingTag: repository + ":go",
		},
	}

	initial := func() map[string]string {
		return map[string]string{entries[0].CandidateTag: rustDigest, entries[1].CandidateTag: goDigest}
	}

	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}

	// The dry run: promotion through the simulating registry, then cleanup
	// through the same preview decorator, against a registry nothing may touch.
	untouched := &tagStore{tags: initial()}

	var preview bytes.Buffer

	simulated := newDryRunRegistry(untouched, &preview)
	if err := runLedgerPromotion(t.Context(), promotionRun{
		reg: simulated, sigCopier: simulated, entries: entries, releaseTag: "v1.2.3", stage: stage, dryRun: true, errPrefix: "test", out: io.Discard,
	}); err != nil {
		t.Fatalf("dry-run promote: %v", err)
	}

	if err := imageledger.Cleanup(t.Context(), simulated, entries, "v1.2.3"); err != nil {
		t.Fatalf("dry-run cleanup: %v", err)
	}

	if len(untouched.operations) != 0 || !maps.Equal(untouched.tags, initial()) {
		t.Fatalf("the dry run mutated the registry: %v %v", untouched.operations, untouched.tags)
	}

	// The wet control, through the same decorators a real run uses.
	registry := &tagStore{tags: initial()}
	if err := runLedgerPromotion(t.Context(), promotionRun{
		reg: auditCopier{Registry: registry, out: io.Discard}, entries: entries, releaseTag: "v1.2.3", stage: stage, errPrefix: "test", out: io.Discard,
	}); err != nil {
		t.Fatalf("promote: %v", err)
	}

	if err := imageledger.Cleanup(t.Context(), auditDeleter{CleanupRegistry: registry, out: io.Discard}, entries, "v1.2.3"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []string{
		"promote " + entries[0].CandidateTag + " -> " + entries[0].FinalTag + " (" + rustDigest + ")",
		"promote " + entries[0].CandidateTag + " -> " + entries[0].MovingTag + " (" + rustDigest + ")",
		"promote " + entries[1].CandidateTag + " -> " + entries[1].FinalTag + " (" + goDigest + ")",
		"promote " + entries[1].CandidateTag + " -> " + entries[1].MovingTag + " (" + goDigest + ")",
		"delete " + entries[0].CandidateTag,
		"delete " + entries[1].CandidateTag,
	}

	if got := registry.operations; !slices.Equal(got, want) {
		t.Fatalf("real run performed %q, want %q", got, want)
	}

	if got := previewOperations(t, preview.String()); !slices.Equal(got, registry.operations) {
		t.Errorf("dry run previewed %q, but the real run performed %q", got, registry.operations)
	}
}
