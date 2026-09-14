// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

// The tags a cleanup or rollback would move, and a digest the previewing
// registry can resolve so the domain's post-copy verification still runs.
const (
	staging = "registry.invalid/owner/image:staging-v1.2.3"
	final   = "registry.invalid/owner/image:v1.2.3"
)

func digestFixture() string { return "sha256:" + strings.Repeat("a", 64) }

// The dry-run decorator is already proved not to touch the registry. What that
// proves is a property of the decorator, and the decorator is not what a
// contributor changes by accident.
//
// `cleanupReg` and `promotionRollbackReg` are the command-level wiring: they
// decide, from one boolean, whether the flow gets the previewing registry or
// the real one. Change either factory to return the real registry regardless of
// the flag and every decorator test still passes, because the decorator is
// still correct — it is simply no longer reached. The flow would then delete
// staging tags on a `--dry-run` cleanup, which is the exact outcome the flag
// exists to prevent.
//
// So these exercise the factories: every mutating method on what they return,
// with the flag set, must reach nothing.

func TestCleanupRegistryFactory_DryRunReachesNothing(t *testing.T) {
	t.Parallel()

	recorder := &recordingRegistry{stubResolver: stubResolver{staging: digestFixture()}}

	registry, err := cleanupReg(&deps.Deps{}, true, recorder)
	if err != nil {
		t.Fatalf("dry-run cleanup registry must not need a forge: %v", err)
	}

	if _, err := registry.ResolveDigest(t.Context(), staging); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if err := registry.DeleteTag(t.Context(), staging); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(recorder.copied)+len(recorder.deleted) != 0 {
		t.Errorf("a --dry-run cleanup reached the registry: copied=%v deleted=%v", recorder.copied, recorder.deleted)
	}
}

func TestPromotionRollbackRegistryFactory_DryRunReachesNothing(t *testing.T) {
	t.Parallel()

	recorder := &recordingRegistry{stubResolver: stubResolver{staging: digestFixture(), final: digestFixture()}}

	registry, err := promotionRollbackReg(&deps.Deps{}, true, recorder)
	if err != nil {
		t.Fatalf("dry-run rollback registry must not need a forge: %v", err)
	}

	if _, err := registry.ResolveDigest(t.Context(), staging); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if err := registry.CopyTag(t.Context(), staging, final); err != nil {
		t.Fatalf("copy: %v", err)
	}

	if err := registry.DeleteTag(t.Context(), final); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(recorder.copied)+len(recorder.deleted) != 0 {
		t.Errorf("a --dry-run rollback reached the registry: copied=%v deleted=%v", recorder.copied, recorder.deleted)
	}
}

// The positive control. Without it the two tests above pass just as well
// against factories that return something inert in every mode — which would be
// a broken tool with a green suite.
//
// A real run needs a forge tag-deleter, and an empty Deps cannot supply one, so
// what is asserted is that the factory REFUSES rather than quietly handing back
// a registry that deletes nothing. A silent no-op there would look like a
// successful cleanup that left every staging tag in place.
func TestRegistryFactories_RealRunRefusesWithoutAForge(t *testing.T) {
	t.Parallel()

	recorder := &recordingRegistry{}

	for _, tc := range []struct {
		name  string
		build func() (any, error)
	}{
		{"cleanup", func() (any, error) { return cleanupReg(&deps.Deps{}, false, recorder) }},
		{"promotion rollback", func() (any, error) { return promotionRollbackReg(&deps.Deps{}, false, recorder) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			registry, err := tc.build()
			if err == nil {
				t.Fatalf("a real run built a registry with no tag deleter: %#v; a cleanup would report success "+
					"having deleted nothing", registry)
			}
		})
	}
}
