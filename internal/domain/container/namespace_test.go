// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidateNamespace_AcceptsOnlyTheOwnersPrefixAndItsSubpaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		in            container.ValidateNamespaceInput
		wantErr       bool
		wantViolation bool
	}{
		// === Valid GHCR ===
		{
			name: "exact prefix",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Repository:       "myorg/myrepo",         //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Registry:         "ghcr.io",              //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				EnforceNamespace: "myorg",                //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
		},
		{
			name: "with -suffix",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo-api",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
		},
		{
			name: "with /subpath",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo/frontend",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
		},
		{
			name: "deeper subpath",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo/sub/path",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
		},

		// === Invalid GHCR ===
		{
			name: "wrong owner",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/evil/myrepo",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
			wantErr: true, wantViolation: true,
		},
		{
			name: "wrong repo",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/different",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
			wantErr: true, wantViolation: true,
		},
		{
			name: "sibling-suffix-then-subpath is rejected",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo-evil/payload",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
			wantErr: true, wantViolation: true,
		},
		{
			name: "missing repo segment",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg",
				Repository:       "myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
			wantErr: true, wantViolation: true,
		},

		// === Non-GHCR registries are skipped ===
		{
			name: "docker.io is skipped",
			in: container.ValidateNamespaceInput{
				ImageName:        "docker.io/anyone/anything",
				Repository:       "myorg/myrepo",
				Registry:         "docker.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				EnforceNamespace: "myorg",
			},
		},
		{
			name: "registry.gitlab.com is skipped",
			in: container.ValidateNamespaceInput{
				ImageName:        "registry.gitlab.com/group/project/image",
				Repository:       "group/project",
				Registry:         "registry.gitlab.com", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				EnforceNamespace: "group",               //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
		},

		// === Repository with org prefix (multi-segment owner) ===
		{
			name: "multi-segment repository's short name is used",
			in: container.ValidateNamespaceInput{
				ImageName:        "ghcr.io/myorg/myrepo",
				Repository:       "diggsweden/myorg/myrepo",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			},
		},

		// === Self-hosted registry: enforced when listed (the adopter path) ===
		// Without EnforceOnRegistries this image would silently pass; an
		// adopter sets it to their registry so the namespace check runs.
		{
			name: "self-hosted registry enforced — valid namespace",
			in: container.ValidateNamespaceInput{
				ImageName:           "harbor.example.gov/myorg/myrepo",
				Repository:          "myorg/myrepo",
				Registry:            "harbor.example.gov",
				EnforceNamespace:    "myorg",
				EnforceOnRegistries: []string{"harbor.example.gov"},
			},
		},
		{
			name: "self-hosted registry enforced — namespace violation caught",
			in: container.ValidateNamespaceInput{
				ImageName:           "harbor.example.gov/evil/myrepo",
				Repository:          "myorg/myrepo",
				Registry:            "harbor.example.gov",
				EnforceNamespace:    "myorg",
				EnforceOnRegistries: []string{"harbor.example.gov"},
			},
			wantErr: true, wantViolation: true,
		},
		{
			name: "registry not in enforce set is skipped even when explicit set is given",
			in: container.ValidateNamespaceInput{
				ImageName:           "ghcr.io/evil/myrepo",
				Repository:          "myorg/myrepo",
				Registry:            "ghcr.io",
				EnforceNamespace:    "myorg",
				EnforceOnRegistries: []string{"harbor.example.gov"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := container.ValidateNamespace(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantViolation {
				var nv *container.NamespaceViolationError
				if !errors.As(err, &nv) {
					t.Errorf("err = %v, want NamespaceViolation", err)
				}
				// A namespace violation is a domain-rule failure → EX_VALIDATION
				// (1), not the unclassified EX_SOFTWARE (70).
				if !errors.Is(err, errs.ErrValidation) {
					t.Errorf("err = %v, want wrapped errs.ErrValidation", err)
				}
			}
		})
	}
}

func TestValidateNamespace_EmptyRepositoryFailsClosed(t *testing.T) {
	t.Parallel()

	for _, image := range []string{
		"ghcr.io/myorg/-evil",
		"ghcr.io/myorg//evil/deeper",
		"ghcr.io/myorg/",
		"ghcr.io/myorg/legit",
		"ghcr.io/other/evil",
	} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()

			err := container.ValidateNamespace(container.ValidateNamespaceInput{
				ImageName:        image,
				Repository:       "",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestValidateNamespace_RegistryCaseDoesNotSkipEnforcement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		registry  string
		enforceOn []string
	}{
		{name: "lowercase default", registry: "ghcr.io"},
		{name: "uppercase registry", registry: "GHCR.IO"},
		{name: "uppercase policy", registry: "ghcr.io", enforceOn: []string{"GHCR.IO"}},
		{name: "both uppercase", registry: "GHCR.IO", enforceOn: []string{"GHCR.IO"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := container.ValidateNamespaceInput{
				Registry: tc.registry, EnforceOnRegistries: tc.enforceOn,
				Repository: "owner/app", EnforceNamespace: "owner",
				ImageName: "ghcr.io/owner/app",
			}
			if err := container.ValidateNamespace(in); err != nil {
				t.Fatalf("valid namespace rejected: %v", err)
			}

			in.ImageName = "ghcr.io/other/app"
			if err := container.ValidateNamespace(in); !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("foreign namespace: err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestNamespaceViolation_ErrorMessage(t *testing.T) {
	t.Parallel()

	v := &container.NamespaceViolationError{
		ImageName:      "ghcr.io/evil/payload",
		ExpectedPrefix: "ghcr.io/myorg/myrepo",
	}

	msg := v.Error()
	if !strings.Contains(msg, "outside the allowed namespace") {
		t.Errorf("error message missing key phrase: %q", msg)
	}

	if !strings.Contains(msg, "ghcr.io/myorg/myrepo") {
		t.Errorf("error message missing expected prefix: %q", msg)
	}
}

// TestValidateNamespace_OwnerCaseIsIgnored pins that a mixed-case owner (the
// forge's github.repository_owner keeps "MyOrg") matches the lowercased image
// name ResolveImageName derives, instead of reading as a violation, and that
// the violation message names the lowercased prefix the image must carry.
func TestValidateNamespace_OwnerCaseIsIgnored(t *testing.T) {
	t.Parallel()

	in := container.ValidateNamespaceInput{
		ImageName:        "ghcr.io/myorg/myrepo",
		Repository:       "MyOrg/MyRepo",
		Registry:         "ghcr.io",
		EnforceNamespace: "MyOrg",
	}
	if err := container.ValidateNamespace(in); err != nil {
		t.Fatalf("ValidateNamespace() = %v, want nil for a mixed-case owner against the lowercased image", err)
	}

	in.ImageName = "ghcr.io/evil/myrepo"

	var violation *container.NamespaceViolationError
	if err := container.ValidateNamespace(in); !errors.As(err, &violation) {
		t.Fatalf("ValidateNamespace() = %v, want a namespace violation for a foreign owner", err)
	}

	if violation.ExpectedPrefix != "ghcr.io/myorg/myrepo" {
		t.Errorf("ExpectedPrefix = %q, want the lowercased prefix the image must actually carry", violation.ExpectedPrefix)
	}
}

// TestValidateNamespace_TreatsDotsInTheRepositoryLiterally pins the quoting.
// A repository name may contain a dot, and in an unquoted pattern that dot
// matches any character -- so "my.repo" would admit a sibling package spelled
// "myXrepo", which anyone in the owner namespace can create.
func TestValidateNamespace_TreatsDotsInTheRepositoryLiterally(t *testing.T) {
	t.Parallel()

	in := container.ValidateNamespaceInput{
		Registry: "ghcr.io", Repository: "myorg/my.repo", EnforceNamespace: "myorg",
	}

	in.ImageName = "ghcr.io/myorg/my.repo"
	if err := container.ValidateNamespace(in); err != nil {
		t.Fatalf("the literal repository was refused: %v", err)
	}

	in.ImageName = "ghcr.io/myorg/myXrepo"
	if err := container.ValidateNamespace(in); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("a sibling matching the dot as a wildcard was accepted: err = %v", err)
	}
}

// TestValidateNamespace_NamesWhichInputIsMissing distinguishes the two
// fail-closed configuration errors, which used to share one message.
func TestValidateNamespace_NamesWhichInputIsMissing(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in   container.ValidateNamespaceInput
		want string
	}{
		"no enforced owner": {
			in:   container.ValidateNamespaceInput{Registry: "ghcr.io", Repository: "myorg/app", ImageName: "ghcr.io/myorg/app"},
			want: "enforced namespace (owner)",
		},
		"no repository": {
			in:   container.ValidateNamespaceInput{Registry: "ghcr.io", EnforceNamespace: "myorg", ImageName: "ghcr.io/myorg/app"},
			want: "repository to check",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := container.ValidateNamespace(tc.in)
			if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want ErrInvalidConfig naming %q", err, tc.want)
			}
		})
	}
}

// TestValidateNamespace_EnforcesEveryConfiguredRegistryAndOnlyThose covers a
// policy list with more than one entry. Every existing case used the default
// or a single registry, so a check that consulted only the first entry passed.
// Registries outside the list are skipped by design -- they run their own
// policy -- and that skip is pinned too, so it stays deliberate.
func TestValidateNamespace_EnforcesEveryConfiguredRegistryAndOnlyThose(t *testing.T) {
	t.Parallel()

	policy := []string{"ghcr.io", "registry.example"}

	for name, tc := range map[string]struct {
		registry, image string
		wantViolation   bool
	}{
		"first registry, foreign owner":  {registry: "ghcr.io", image: "ghcr.io/other/app", wantViolation: true},
		"second registry, foreign owner": {registry: "registry.example", image: "registry.example/other/app", wantViolation: true},
		"second registry, correct owner": {registry: "registry.example", image: "registry.example/myorg/app"},
		"unlisted registry is skipped":   {registry: "quay.io", image: "quay.io/other/app"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := container.ValidateNamespace(container.ValidateNamespaceInput{
				ImageName: tc.image, Repository: "myorg/app", Registry: tc.registry,
				EnforceNamespace: "myorg", EnforceOnRegistries: policy,
			})

			if !tc.wantViolation {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}

				return
			}

			// The whole violation, not a substring of its message: the
			// offending image and the prefix it should have carried are the
			// two fields an operator acts on.
			var violation *container.NamespaceViolationError
			if !errors.As(err, &violation) {
				t.Fatalf("err = %v, want *NamespaceViolationError", err)
			}

			want := container.NamespaceViolationError{ImageName: tc.image, ExpectedPrefix: tc.registry + "/myorg/app"}
			if *violation != want {
				t.Errorf("violation = %+v, want %+v", *violation, want)
			}
		})
	}
}
