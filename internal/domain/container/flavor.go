// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ValidateFlavor accepts the docker/metadata-action FLAVOR input and
// rejects anything beyond `latest=false`. The script never auto-emits a
// :latest tag, and prefix=/suffix=/onlatest= would change tag shapes
// silently — refusing them keeps the contract honest.
func ValidateFlavor(flavor string) error {
	if flavor == "" {
		return nil
	}

	for _, raw := range strings.Split(flavor, "\n") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		switch entry {
		case "latest=false":
			// the only flavor we accept
		case "latest=auto", "latest=true":
			return fmt.Errorf("FLAVOR latest=auto/true is not supported by this script (no auto :latest tag): %w", errs.ErrUnsupported)
		default:
			key, _, ok := strings.Cut(entry, "=")
			if !ok {
				return fmt.Errorf("unknown FLAVOR entry: %s: %w", entry, errs.ErrUsage)
			}

			switch key {
			case "prefix", "suffix", "onlatest":
				return fmt.Errorf("FLAVOR %s is not supported by this script: %w", key, errs.ErrUnsupported)
			default:
				return fmt.Errorf("unknown FLAVOR entry: %s: %w", entry, errs.ErrUsage)
			}
		}
	}

	return nil
}
