// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
)

// ValidResultName reports whether a stage or job name can be used verbatim as
// one filename stem. Result names are data, not paths: <stage>-result.json and
// jobs/<job>.json are flat records. No case folding or sanitizing is performed.
func ValidResultName(name string) bool {
	return strings.TrimSpace(name) != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`) && artifact.ValidateName(name) == nil
}
