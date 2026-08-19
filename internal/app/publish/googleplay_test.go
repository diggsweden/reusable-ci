// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestGooglePlayCheckCredentials_AcceptsValidServiceAccount(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	in := apppublish.GooglePlayCheckCredentialsInput{
		ServiceAccountJSON: `{
			"type":"service_account",
			"client_email":"x@y.iam.gserviceaccount.com",
			"private_key":"k"
		}`,
	}
	if err := apppublish.GooglePlayCheckCredentials(context.Background(), &out, in); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "Service account secret is configured") {
		t.Errorf("missing success log: %s", out.String())
	}
}

func TestGooglePlayCheckCredentials_Refusals(t *testing.T) {
	t.Parallel()

	// Escaped newlines, as a real service-account file carries them.
	const privateKey = `-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg-SECRET-BODY\n-----END PRIVATE KEY-----\n`

	for _, tc := range []struct {
		name    string
		json    string
		wantErr error
		wantMsg string
	}{
		{
			name:    "unset",
			wantErr: errs.ErrMissingInput,
			wantMsg: "service account JSON is required",
		},
		{
			name:    "wrong credential type",
			json:    `{"type":"user","client_email":"x@y","private_key":"` + privateKey + `"}`,
			wantErr: errs.ErrValidation,
			wantMsg: `want "service_account"`,
		},
		{
			name:    "missing client_email",
			json:    `{"type":"service_account","private_key":"` + privateKey + `"}`,
			wantErr: errs.ErrValidation,
			wantMsg: "missing client_email",
		},
		{
			name:    "missing private_key",
			json:    `{"type":"service_account","client_email":"x@y.iam.gserviceaccount.com"}`,
			wantErr: errs.ErrValidation,
			wantMsg: "missing private_key",
		},
		{
			name:    "not json at all",
			json:    `not json ` + privateKey,
			wantErr: errs.ErrValidation,
			wantMsg: "parse service account JSON",
		},
		{
			name:    "truncated json",
			json:    `{"type":"service_account","private_key":"` + privateKey,
			wantErr: errs.ErrValidation,
			wantMsg: "parse service account JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := apppublish.GooglePlayCheckCredentials(context.Background(), &out, apppublish.GooglePlayCheckCredentialsInput{
				ServiceAccountJSON: tc.json,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantMsg)
			}

			// The body being validated is a service-account credential.
			// Neither the error nor the log may echo any part of it --
			// both reach the CI log, and a private key printed there is
			// a key that has to be rotated.
			for _, fragment := range []string{"SECRET-BODY", "BEGIN PRIVATE KEY"} {
				if strings.Contains(err.Error(), fragment) {
					t.Errorf("error echoed key material (%q): %v", fragment, err)
				}

				if strings.Contains(out.String(), fragment) {
					t.Errorf("log echoed key material (%q): %s", fragment, out.String())
				}
			}
		})
	}
}
