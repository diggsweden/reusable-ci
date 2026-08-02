// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// BaseLineagePredicate writes the standard SLSA Provenance v1.0 predicate used
// for forgejo-ci base-image lineage attestations.
func BaseLineagePredicate(out io.Writer, in provenance.BaseLineageInput) error {
	body, err := provenance.BaseLineagePredicate(in)
	if err != nil {
		return err
	}

	if _, err := out.Write(body); err != nil {
		return fmt.Errorf("write base lineage predicate: %w", err)
	}

	return nil
}
