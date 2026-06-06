// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/publish"
)

func TestValidateRegistryAuth_OKWithCITokenAndDefaultRegistry(t *testing.T) {
	res := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken: true, Registry: "ghcr.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Errorf("expected clean, got %+v", res)
	}
}

func TestValidateRegistryAuth_ErrorsWhenCustomAuthNoPassword(t *testing.T) {
	res := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken: false, Registry: "ghcr.io", HasPassword: false,
	})
	if len(res.Errors) == 0 {
		t.Fatal("expected error when use-ci-token=false and no password")
	}

	if !strings.Contains(res.Errors[0], "registry-password") {
		t.Errorf("error message = %q", res.Errors[0])
	}
}

func TestValidateRegistryAuth_WarnsOnCITokenWithCustomRegistry(t *testing.T) {
	res := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken: true, Registry: "https://npm.pkg.github.com",
		ExpectedRegistry: "ghcr.io", HasPassword: false,
	})
	if len(res.Errors) != 0 {
		t.Errorf("did not expect errors: %+v", res.Errors)
	}

	if len(res.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(res.Warnings))
	}
}

func TestValidateRegistryAuth_DefaultsExpectedRegistryToGHCR(t *testing.T) {
	res := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken: true, Registry: "https://other.example",
	})
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "non-ghcr.io") {
		t.Errorf("expected ghcr.io default in warning, got %v", res.Warnings)
	}
}

func TestValidateRegistryAuth_OKWithCustomAuthAndPasswordForCommonRegistries(t *testing.T) {
	for _, registry := range []string{
		"ghcr.io",
		"docker.io",
		"registry.example.com",
		"123456789012.dkr.ecr.eu-west-1.amazonaws.com",
		"myregistry.azurecr.io",
		"gcr.io",
		"quay.io",
	} {
		res := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
			UseCIToken:  false,
			Registry:    registry,
			HasPassword: true,
		})
		if len(res.Errors) != 0 || len(res.Warnings) != 0 {
			t.Errorf("registry %q: expected clean, got %+v", registry, res)
		}
	}
}

func TestValidateRegistryAuth_CustomExpectedRegistryCases(t *testing.T) {
	ok := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken:       true,
		Registry:         "custom.registry.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ExpectedRegistry: "custom.registry.io",
	})
	if len(ok.Errors) != 0 || len(ok.Warnings) != 0 {
		t.Errorf("matching custom expected registry should be clean, got %+v", ok)
	}

	warn := publish.ValidateRegistryAuth(publish.RegistryAuthInput{
		UseCIToken:       true,
		Registry:         "other.registry.io",
		ExpectedRegistry: "custom.registry.io",
	})
	if len(warn.Errors) != 0 {
		t.Errorf("did not expect errors: %+v", warn.Errors)
	}

	if len(warn.Warnings) == 0 || !strings.Contains(warn.Warnings[0], "non-custom.registry.io") {
		t.Errorf("expected custom expected-registry warning, got %+v", warn.Warnings)
	}
}
