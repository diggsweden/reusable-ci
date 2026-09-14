// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type orderedRegistry struct {
	events []string
	fail   error
}

func (r *orderedRegistry) Write(body []byte) (int, error) {
	r.events = append(r.events, "log:"+string(body))

	return len(body), nil
}
func (r *orderedRegistry) ResolveDigest(context.Context, string) (string, error) {
	return "sha256:fixture", r.fail
}
func (r *orderedRegistry) CopyTag(_ context.Context, source, dest string) error {
	r.events = append(r.events, "copy:"+source+"->"+dest)

	return r.fail
}
func (r *orderedRegistry) DeleteTag(_ context.Context, ref string) error {
	r.events = append(r.events, "delete:"+ref)

	return r.fail
}

func TestAuditOrderBoundary_NarratesBeforeEveryMutation(t *testing.T) {
	t.Parallel()

	for _, failure := range []error{nil, errs.ErrDependencyUnavailable} {
		for _, operation := range []string{"copy", "delete", "restore", "rollback-delete"} {
			r := &orderedRegistry{fail: failure}

			var (
				err  error
				want []string
			)

			switch operation {
			case "copy":
				err = (auditCopier{Registry: r, out: r}).CopyTag(t.Context(), "source", "dest")
				want = []string{"log:ledger: promoting source -> dest\n", "copy:source->dest"}
			case "delete":
				err = (auditDeleter{CleanupRegistry: r, out: r}).DeleteTag(t.Context(), "ref")
				want = []string{"log:ledger: deleting ref\n", "delete:ref"}
			case "restore":
				err = (auditPromotionRollbackRegistry{PromotionRollbackRegistry: r, out: r}).CopyTag(t.Context(), "source", "dest")
				want = []string{"log:ledger: restoring source -> dest\n", "copy:source->dest"}
			case "rollback-delete":
				err = (auditPromotionRollbackRegistry{PromotionRollbackRegistry: r, out: r}).DeleteTag(t.Context(), "ref")
				want = []string{"log:ledger: deleting ref\n", "delete:ref"}
			}

			require.ErrorIs(t, err, failure)
			require.Equal(t, want, r.events)
		}
	}
}

func TestDryRunBoundary_DoesNotCallUnderlyingMutationMethods(t *testing.T) {
	t.Parallel()

	for _, failure := range []error{nil, errs.ErrDependencyUnavailable} {
		r := &orderedRegistry{fail: failure}
		preview := newDryRunRegistry(r, io.Discard)
		require.ErrorIs(t, preview.CopyTag(t.Context(), "source", "dest"), failure)
		require.ErrorIs(t, preview.CopyWithSignatures(t.Context(), "source", "other"), failure)
		require.NoError(t, preview.DeleteTag(t.Context(), "ref"))
		require.Empty(t, r.events)
	}
}
