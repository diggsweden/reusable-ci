// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
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

			// Recorded limit: the rule is "carries an @sha256: marker",
			// not "carries a well-formed digest". A malformed digest gets
			// past this guard and is refused later by cosign or the
			// registry, with a worse message. The package has ValidDigest
			// if this is ever tightened.
			if err := tc.with("registry.example/app@sha256:"); err != nil {
				t.Errorf("the guard now rejects a malformed digest (%v) — it can be tightened with ValidDigest; update this comment", err)
			}
		})
	}
}

func TestImageRequests_KeylessAndKeyAreExclusive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		bothSet, neither  func() error
		keylessNoIdentity func() error
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
			// Without an identity constraint a keyless verify accepts a
			// signature from anyone.
			keylessNoIdentity: func() error {
				return container.ImageVerifyRequest{ImageRef: digestRef, Keyless: true, CertOIDCIssuer: "https://i"}.Validate()
			},
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
			keylessNoIdentity: func() error {
				return container.AttestationVerifyRequest{
					ImageRef: digestRef, PredicateType: "cyclonedx", Keyless: true, CertIdentityRegexp: "^https://x/",
				}.Validate()
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

			if tc.keylessNoIdentity != nil {
				if err := tc.keylessNoIdentity(); !errors.Is(err, errs.ErrUsage) {
					t.Errorf("keyless without a full identity constraint: err = %v, want ErrUsage", err)
				}
			}
		})
	}
}
