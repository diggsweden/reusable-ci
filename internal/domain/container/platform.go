// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Platform is an OCI image platform: os/arch with an optional variant
// (e.g. linux/arm/v7). This is deliberately NOT the Go build target pair
// (domain/build.SplitPlatform) — OCI platforms carry a variant component
// Go targets don't have. Callers with a stricter contract (image evidence
// scans take exactly os/arch) layer their own rejection on top, the same
// way ecosystem checks layer on SplitPlatform.
type Platform struct {
	OS      string
	Arch    string
	Variant string
}

// ParsePlatform parses "os/arch" or "os/arch/variant"; the format rule for
// OCI platforms lives here and nowhere else.
func ParsePlatform(platform string) (Platform, error) {
	parts := strings.Split(platform, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return Platform{}, fmt.Errorf("platform must be os/arch or os/arch/variant: %s: %w", platform, errs.ErrUsage)
	}

	parsed := Platform{OS: parts[0], Arch: parts[1]}
	if len(parts) == 3 {
		parsed.Variant = parts[2]
	}

	return parsed, nil
}
