// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func authOf(t *testing.T, body []byte, registry string) string {
	t.Helper()

	var doc struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	return doc.Auths[registry].Auth
}

func TestMergeAuth_NewConfig(t *testing.T) {
	t.Parallel()

	out, err := container.MergeAuth(nil, "ghcr.io", "alice", "s3cret")
	if err != nil {
		t.Fatal(err)
	}

	want := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if got := authOf(t, out, "ghcr.io"); got != want {
		t.Errorf("auth = %q, want %q", got, want)
	}
}

func TestMergeAuth_PreservesOtherRegistries(t *testing.T) {
	t.Parallel()

	existing := []byte(`{"auths":{"ghcr.io":{"auth":"keep"}},"credsStore":"x"}`)

	out, err := container.MergeAuth(existing, "codeberg.org", "bot", "tok")
	if err != nil {
		t.Fatal(err)
	}

	if got := authOf(t, out, "ghcr.io"); got != "keep" {
		t.Errorf("existing ghcr.io entry clobbered: %q", got)
	}

	if got := authOf(t, out, "codeberg.org"); got != base64.StdEncoding.EncodeToString([]byte("bot:tok")) {
		t.Errorf("codeberg.org not added: %q", got)
	}

	// Unrelated top-level fields survive.
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	if doc["credsStore"] != "x" {
		t.Errorf("credsStore dropped: %v", doc["credsStore"])
	}
}

func TestMergeAuth_RejectsMissingFieldsAndBadJSON(t *testing.T) {
	t.Parallel()

	if _, err := container.MergeAuth(nil, "ghcr.io", "", "p"); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty username should be ErrUsage, got %v", err)
	}

	if _, err := container.MergeAuth([]byte("{not json"), "ghcr.io", "u", "p"); !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("bad existing config should be ErrMalformedInput, got %v", err)
	}
}
