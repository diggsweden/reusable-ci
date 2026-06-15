// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/clicolor"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// RegistryAuth applies the pure auth-decision table from
// domain/publish.ValidateRegistryAuth, prints warnings to stderr as
// `::warning::` lines, and returns an error when the configuration is
// invalid.
func RegistryAuth(_ context.Context, w, stderr io.Writer, annot output.Annotator, in publish.RegistryAuthInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	_, _ = fmt.Fprintf(w, "%s Registry authentication configuration is valid\n", clicolor.Check(w))

	return nil
}
