// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func TestValidateSignFlags_GPGKeyFilesPerMethod(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		method   domainrelease.SignMethod
		keyRef   string
		keyFile  string
		passFile string
		wantErr  string // substring; "" means no error
	}{
		{
			name:    "gpg allows private-key-file",
			method:  domainrelease.SignMethodGPG,
			keyFile: "key.asc",
		},
		{
			name:     "gpg allows passphrase-file",
			method:   domainrelease.SignMethodGPG,
			passFile: "pass.txt",
		},
		{
			// kms needs --key; supplying it plus a GPG key-file is the mix we reject.
			name:    "kms forbids private-key-file",
			method:  domainrelease.SignMethodKMS,
			keyRef:  "hashivault://transit/keys/release",
			keyFile: "key.asc",
			wantErr: "--private-key-file is forbidden for --method=kms",
		},
		{
			name:     "sigstore forbids passphrase-file",
			method:   domainrelease.SignMethodSigstore,
			passFile: "pass.txt",
			wantErr:  "--passphrase-file is forbidden for --method=sigstore",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateSignFlags(tc.method, tc.keyRef, "", tc.keyFile, tc.passFile)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			// A method/flag combination that cannot work is configuration the
			// operator has to change, so it classifies as ErrInvalidConfig.
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
