// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// SplitPlatform splits a "GOOS/GOARCH" platform string into its two trimmed,
// non-empty components, returned as (goos, goarch, error). It validates only
// the shape; the ecosystem-specific "is this a known target" check
// (IsKnownGoPlatform / IsKnownCargoPlatform) is layered on by the caller, so
// the format rule lives in one place.
func SplitPlatform(platform string) (string, string, error) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid platform %q, expected GOOS/GOARCH: %w", platform, errs.ErrUsage)
	}

	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
