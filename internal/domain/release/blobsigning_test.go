// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// Both request validators had no direct test — only whatever the cosign
// adapter's argv tests happened to exercise. They run "before any
// subprocess runs", so they are the last chance to catch a request that
// would ask cosign to do the wrong thing.

func TestBlobSignRequest_Validate(t *testing.T) {
	t.Parallel()

	keyless := release.BlobSignRequest{Artifact: "app.tgz", BundlePath: "app.tgz.bundle", Keyless: true}
	kms := release.BlobSignRequest{Artifact: "app.tgz", BundlePath: "app.tgz.bundle", KeyRef: "awskms:///alias/k"}

	for _, tc := range []struct {
		name    string
		in      release.BlobSignRequest
		wantErr bool
	}{
		{name: "keyless", in: keyless},
		{name: "kms", in: kms},
		{name: "keyless with an issuer", in: release.BlobSignRequest{Artifact: "a", BundlePath: "b", Keyless: true, OIDCIssuer: "https://issuer"}},

		{name: "no artifact", in: release.BlobSignRequest{BundlePath: "b", Keyless: true}, wantErr: true},
		{name: "no bundle path", in: release.BlobSignRequest{Artifact: "a", Keyless: true}, wantErr: true},

		// The two modes are mutually exclusive. Both set is ambiguous:
		// cosign would take the key path and the run would look keyless
		// in the logs while producing a key-signed bundle.
		{name: "keyless with a key", in: release.BlobSignRequest{Artifact: "a", BundlePath: "b", Keyless: true, KeyRef: "awskms:///alias/k"}, wantErr: true},
		{name: "neither keyless nor a key", in: release.BlobSignRequest{Artifact: "a", BundlePath: "b"}, wantErr: true},

		// An issuer outside keyless mode is a sign the caller believes
		// it is signing keylessly when it is not.
		{name: "issuer without keyless", in: release.BlobSignRequest{Artifact: "a", BundlePath: "b", KeyRef: "awskms:///alias/k", OIDCIssuer: "https://issuer"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.in.Validate()
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr && !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage", err)
			}
		})
	}
}

func TestBlobVerifyRequest_Validate(t *testing.T) {
	t.Parallel()

	keyless := release.BlobVerifyRequest{
		Artifact: "app.tgz", BundlePath: "app.tgz.bundle", Keyless: true,
		CertIdentityRegexp: "^https://github.com/org/", CertOIDCIssuer: "https://token.actions.githubusercontent.com",
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*release.BlobVerifyRequest)
		wantErr bool
	}{
		{name: "keyless with both constraints"},
		{
			name: "kms with a key",
			mutate: func(r *release.BlobVerifyRequest) {
				r.Keyless, r.CertIdentityRegexp, r.CertOIDCIssuer = false, "", ""
				r.KeyRef = "./pubkey.pem"
			},
		},

		{name: "no artifact", mutate: func(r *release.BlobVerifyRequest) { r.Artifact = "" }, wantErr: true},
		{name: "no bundle path", mutate: func(r *release.BlobVerifyRequest) { r.BundlePath = "" }, wantErr: true},

		// The two identity constraints are what make a keyless verify
		// mean anything. Without the regexp cosign accepts a signature
		// from any identity; without the issuer, from any issuer whose
		// subject happens to match. Verifying that *something* signed
		// the artifact is not the same as verifying that the right
		// thing did.
		{name: "keyless without an identity regexp", mutate: func(r *release.BlobVerifyRequest) { r.CertIdentityRegexp = "" }, wantErr: true},
		{name: "keyless without an issuer", mutate: func(r *release.BlobVerifyRequest) { r.CertOIDCIssuer = "" }, wantErr: true},

		{name: "keyless with a key", mutate: func(r *release.BlobVerifyRequest) { r.KeyRef = "./pubkey.pem" }, wantErr: true},
		{
			name: "non-keyless without a key",
			mutate: func(r *release.BlobVerifyRequest) {
				r.Keyless, r.CertIdentityRegexp, r.CertOIDCIssuer = false, "", ""
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := keyless
			if tc.mutate != nil {
				tc.mutate(&in)
			}

			err := in.Validate()
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr && !errors.Is(err, errs.ErrUsage) {
				t.Errorf("err = %v, want ErrUsage", err)
			}
		})
	}
}
