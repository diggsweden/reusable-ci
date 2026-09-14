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

const (
	digestRef = "registry.example/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tagRef    = "registry.example/app:v1.2.3"
)

// The four image signing/verification request validators had no direct
// test — only whatever the cosign adapter's argv tests exercised on the
// way past. Each runs before any subprocess, and each carries the same
// digest-pinning rule, which is the one that matters: signing or
// verifying a mutable tag proves nothing about the image that was built,
// because the tag can move between the two.

func TestImageRequests_RequireADigestPinnedRef(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		with func(ref string) error
	}{
		{
			name: "sign",
			with: func(ref string) error {
				return container.ImageSignRequest{ImageRef: ref, KeyRef: "awskms:///k"}.Validate()
			},
		},
		{
			name: "attest",
			with: func(ref string) error {
				return container.ImageAttestRequest{
					ImageRef: ref, PredicateType: "cyclonedx", PredicatePath: "sbom.json", KeyRef: "awskms:///k",
				}.Validate()
			},
		},
		{
			name: "verify",
			with: func(ref string) error {
				return container.ImageVerifyRequest{ImageRef: ref, KeyRef: "./pub.pem"}.Validate()
			},
		},
		{
			name: "verify-attestation",
			with: func(ref string) error {
				return container.AttestationVerifyRequest{
					ImageRef: ref, PredicateType: "cyclonedx", KeyRef: "./pub.pem",
				}.Validate()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.with(digestRef); err != nil {
				t.Fatalf("digest ref rejected: %v", err)
			}

			if err := tc.with(tagRef); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("tag ref: err = %v, want ErrUsage", err)
			}

			if err := tc.with(""); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("empty ref: err = %v, want ErrUsage", err)
			}

			for _, ref := range []string{
				"registry.example/app@sha256:",
				"registry.example/app@sha256:" + strings.Repeat("a", 63),
				"registry.example/app@sha256:" + strings.Repeat("a", 65),
				"registry.example/app@sha256:" + strings.Repeat("g", 64),
				"registry.example/app@sha256:" + strings.Repeat("A", 64),
				"registry.example/owner/../app@sha256:" + strings.Repeat("a", 64),
				"owner/app@sha256:" + strings.Repeat("a", 64),
			} {
				if err := tc.with(ref); !errors.Is(err, errs.ErrUsage) {
					t.Errorf("invalid digest reference accepted: %v", err)
				}
			}

			if err := tc.with("registry.example/app:source@sha256:" + strings.Repeat("a", 64)); err != nil {
				t.Errorf("valid tag-plus-digest rejected: %v", err)
			}
		})
	}
}

func TestImageRequests_KeylessAndKeyAreExclusive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name             string
		bothSet, neither func() error
	}{
		{
			name: "sign",
			bothSet: func() error {
				return container.ImageSignRequest{ImageRef: digestRef, Keyless: true, KeyRef: "awskms:///k"}.Validate()
			},
			neither: func() error { return container.ImageSignRequest{ImageRef: digestRef}.Validate() },
		},
		{
			name: "attest",
			bothSet: func() error {
				return container.ImageAttestRequest{
					ImageRef: digestRef, PredicateType: "cyclonedx", PredicatePath: "sbom.json",
					Keyless: true, KeyRef: "awskms:///k",
				}.Validate()
			},
			neither: func() error {
				return container.ImageAttestRequest{ImageRef: digestRef, PredicateType: "cyclonedx", PredicatePath: "sbom.json"}.Validate()
			},
		},
		{
			name: "verify",
			bothSet: func() error {
				return container.ImageVerifyRequest{
					ImageRef: digestRef, Keyless: true,
					CertIdentityRegexp: "^https://x/", CertOIDCIssuer: "https://i", KeyRef: "./pub.pem",
				}.Validate()
			},
			neither: func() error { return container.ImageVerifyRequest{ImageRef: digestRef}.Validate() },
		},
		{
			name: "verify-attestation",
			bothSet: func() error {
				return container.AttestationVerifyRequest{
					ImageRef: digestRef, PredicateType: "cyclonedx", Keyless: true,
					CertIdentityRegexp: "^https://x/", CertOIDCIssuer: "https://i", KeyRef: "./pub.pem",
				}.Validate()
			},
			neither: func() error {
				return container.AttestationVerifyRequest{ImageRef: digestRef, PredicateType: "cyclonedx"}.Validate()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Both set is ambiguous rather than redundant: cosign takes
			// the key path, so the run would read as keyless in the logs
			// while producing a key-signed result.
			if err := tc.bothSet(); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("keyless with a key: err = %v, want ErrUsage", err)
			}

			if err := tc.neither(); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("neither keyless nor a key: err = %v, want ErrUsage", err)
			}
		})
	}
}

func TestImageVerification_KeylessIdentityRequirements(t *testing.T) {
	t.Parallel()

	for _, operation := range []struct {
		name     string
		validate func(identity, issuer string) error
	}{
		{"image", func(identity, issuer string) error {
			return container.ImageVerifyRequest{ImageRef: digestRef, Keyless: true,
				CertIdentityRegexp: identity, CertOIDCIssuer: issuer}.Validate()
		}},
		{"attestation", func(identity, issuer string) error {
			return container.AttestationVerifyRequest{ImageRef: digestRef, PredicateType: "cyclonedx", Keyless: true,
				CertIdentityRegexp: identity, CertOIDCIssuer: issuer}.Validate()
		}},
	} {
		for _, tc := range []struct{ name, identity, issuer, missing string }{
			{"valid", "^https://builder.example/release$", "https://issuer.example", ""},
			{"missing-identity", "", "https://issuer.example", "cert-identity-regexp is empty"},
			{"missing-issuer", "^https://builder.example/release$", "", "cert-oidc-issuer is empty"},
		} {
			t.Run(operation.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				err := operation.validate(tc.identity, tc.issuer)
				if tc.missing == "" {
					if err != nil {
						t.Fatalf("valid keyless request refused: %v", err)
					}
				} else if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), tc.missing) {
					t.Fatalf("error = %v, want usage refusal for %q", err, tc.missing)
				}
			})
		}
	}
}

func TestAttestationRequests_PredicateRequirements(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		validate func(predicateType, predicatePath string) error
	}{
		{"attest", func(predicateType, predicatePath string) error {
			return container.ImageAttestRequest{ImageRef: digestRef, Keyless: true,
				PredicateType: predicateType, PredicatePath: predicatePath}.Validate()
		}},
		{"verify", func(predicateType, _ string) error {
			return container.AttestationVerifyRequest{ImageRef: digestRef, Keyless: true, PredicateType: predicateType,
				CertIdentityRegexp: "^https://builder.example/release$", CertOIDCIssuer: "https://issuer.example"}.Validate()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.validate("cyclonedx", "fixture-predicate.json"); err != nil {
				t.Fatalf("valid predicate refused: %v", err)
			}

			if err := tc.validate("", "fixture-predicate.json"); !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "predicate type is empty") {
				t.Fatalf("missing type error = %v", err)
			}

			err := tc.validate("cyclonedx", "")
			if tc.name == "verify" {
				if err != nil {
					t.Fatalf("registry attestation verification must not require a local predicate file: %v", err)
				}
			} else if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "predicate path is empty") {
				t.Fatalf("missing path error = %v", err)
			}
		})
	}
}
