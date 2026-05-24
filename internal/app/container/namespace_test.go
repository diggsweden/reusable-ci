// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"errors"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
)

func TestValidateNamespace_WrapsDomainValidation(t *testing.T) {
	t.Parallel()

	if err := appcontainer.ValidateNamespace(domaincontainer.ValidateNamespaceInput{
		ImageName:        "ghcr.io/diggsweden/reusable-ci/runtime:latest",
		Repository:       "diggsweden/reusable-ci",
		Registry:         "ghcr.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		EnforceNamespace: "diggsweden",
	}); err != nil {
		t.Fatalf("ValidateNamespace: %v", err)
	}

	err := appcontainer.ValidateNamespace(domaincontainer.ValidateNamespaceInput{
		ImageName:        "ghcr.io/other/repo/runtime:latest",
		Repository:       "diggsweden/reusable-ci",
		Registry:         "ghcr.io",
		EnforceNamespace: "diggsweden",
	})

	var violation *domaincontainer.NamespaceViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("err = %v, want NamespaceViolationError", err)
	}
}
