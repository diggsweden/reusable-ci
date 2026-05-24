// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
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
				Repository:       "myorg/myrepo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Registry:         "ghcr.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				EnforceNamespace: "myorg", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
				EnforceNamespace: "group", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
