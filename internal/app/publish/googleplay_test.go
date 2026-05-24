// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
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

func TestGooglePlayCheckCredentials_RejectsMissing(t *testing.T) {
	t.Parallel()

	err := apppublish.GooglePlayCheckCredentials(context.Background(), &bytes.Buffer{}, apppublish.GooglePlayCheckCredentialsInput{})
	if err == nil || !strings.Contains(err.Error(), "service account JSON is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestGooglePlayCheckCredentials_RejectsWrongShape(t *testing.T) {
	t.Parallel()

	in := apppublish.GooglePlayCheckCredentialsInput{
		ServiceAccountJSON: `{"type":"user","client_email":"x@y","private_key":"k"}`,
	}

	err := apppublish.GooglePlayCheckCredentials(context.Background(), &bytes.Buffer{}, in)
	if err == nil || !strings.Contains(err.Error(), `type "user"`) {
		t.Fatalf("err = %v", err)
	}
}
