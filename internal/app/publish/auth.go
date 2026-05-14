// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// RegistryAuth applies the pure auth-decision table from
// domain/publish.ValidateRegistryAuth, prints warnings to stderr as
// `::warning::` lines, and returns an error when the configuration is
// invalid. Mirrors scripts/registry/validate-auth.sh.
func RegistryAuth(_ context.Context, stdout, stderr io.Writer, annot output.Annotator, in publish.RegistryAuthInput) error {
	res := publish.ValidateRegistryAuth(in)
	for _, w := range res.Warnings {
		annot.Warningf("%s", w)
	}
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			annot.Errorf("%s", e)
		}
		return fmt.Errorf("%s: %w", res.Errors[0], errs.ErrPermissionDenied)
	}
	fmt.Fprintln(stdout, "✓ Registry authentication configuration is valid")
	return nil
}
