// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"github.com/diggsweden/reusable-ci/internal/domain/container"
)

// ValidateNamespace returns nil when the image lives in the allowed
// namespace, or a *container.NamespaceViolationError describing the
// violation when it doesn't. Non-ghcr.io registries are silently
// accepted — validation is registry-specific by design.
func ValidateNamespace(in container.ValidateNamespaceInput) error {
	return container.ValidateNamespace(in)
}
