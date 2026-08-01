// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
)

// gpgCleaner is the slice of adapter/gpg.Adapter that GPGCleanup needs.
// Defining a small interface keeps app-layer tests fake-able without
// importing the adapter or invoking a real gpg.
type gpgCleaner interface {
	DeleteSecretKey(ctx context.Context, fingerprint string)
	DeleteKey(ctx context.Context, fingerprint string)
	KillAgent(ctx context.Context)
}

// GPGCleanup removes the imported keys and stops gpg-agent. Idempotent:
// passing an empty fingerprint is a successful no-op (the import step
// never ran or was skipped). Errors during delete are intentionally
// swallowed — this is post-step cleanup wired as `if: always()`.
func GPGCleanup(ctx context.Context, gpg gpgCleaner, fingerprint string, out io.Writer) {
	if fingerprint == "" {
		_, _ = fmt.Fprintln(out, "No fingerprint supplied — nothing to clean up.")

		return
	}

	if gpg == nil {
		_, _ = fmt.Fprintln(out, "No gpg cleaner supplied — nothing to clean up.")

		return
	}

	_, _ = fmt.Fprintf(out, "Removing GPG key %s\n", fingerprint)
	gpg.DeleteSecretKey(ctx, fingerprint)
	gpg.DeleteKey(ctx, fingerprint)

	_, _ = fmt.Fprintln(out, "Killing gpg-agent")

	gpg.KillAgent(ctx)
}
