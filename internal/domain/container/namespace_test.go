// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidateNamespace(t *testing.T) {
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

// TestValidateNamespace_EmptyRepositoryFailsOpen records a gap.
//
// The expected prefix is built as "<registry>/<namespace>/<repo-short>".
// With an empty Repository the prefix ends in a bare slash, and the
// optional "-suffix" / "/subpath" tail then matches anything shaped like
// "<registry>/<namespace>/-<x>" or "<registry>/<namespace>//<x>" -- so
// the check passes images it exists to refuse.
//
// Reach is narrow. The shipped workflow invokes the command with no
// flags and lets the env sources fill them, and an unset or empty
// $GITHUB_REPOSITORY fails the Required check before this code runs.
// It takes an explicit `--repository ""` on the command line, which a
// consumer interpolating an unset variable would produce.
//
// The direction is what makes it worth writing down: a guard on an
// empty input should refuse, not accept. See docs/open-questions.md.
func TestValidateNamespace_EmptyRepositoryFailsOpen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		image        string
		wantAccepted bool
	}{
		{image: "ghcr.io/myorg/-evil", wantAccepted: true},
		{image: "ghcr.io/myorg//evil/deeper", wantAccepted: true},
		{image: "ghcr.io/myorg/", wantAccepted: true},

		// A plain sibling is still refused, so the hole is specifically
		// the suffix/subpath tail rather than the check being disabled.
		{image: "ghcr.io/myorg/legit", wantAccepted: false},
		{image: "ghcr.io/other/evil", wantAccepted: false},
	} {
		t.Run(tc.image, func(t *testing.T) {
			t.Parallel()

			err := container.ValidateNamespace(container.ValidateNamespaceInput{
				ImageName:        tc.image,
				Repository:       "",
				Registry:         "ghcr.io",
				EnforceNamespace: "myorg",
			})
			if accepted := err == nil; accepted != tc.wantAccepted {
				t.Errorf("accepted = %v, want %v (err = %v); if an empty repository is now refused outright, update docs/open-questions.md", accepted, tc.wantAccepted, err)
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
	if !contains(msg, "outside the allowed namespace") {
		t.Errorf("error message missing key phrase: %q", msg)
	}

	if !contains(msg, "ghcr.io/myorg/myrepo") {
		t.Errorf("error message missing expected prefix: %q", msg)
	}
}

// tiny helper used by Error() check above to avoid a strings import in the test
// (purely cosmetic — could use strings.Contains too).
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}
