// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

// TestMergeAuth_ReauthenticatingReplacesTheCredential covers the case the
// other merge tests do not: a registry that already has an entry. Logging
// in again must replace the credential rather than add a second one.
//
// It also pins the documented field preservation -- "a prior config,
// whose other registries and fields are preserved untouched" -- which for
// an entry being re-authenticated includes identitytoken. That has a
// consequence worth knowing about; see docs/open-questions.md.
func TestMergeAuth_ReauthenticatingReplacesTheCredential(t *testing.T) {
	t.Parallel()

	existing := []byte(`{"auths":{"ghcr.io":{"auth":"b2xkOnBhc3M=","identitytoken":"stale-token","email":"a@b.c"}}}`)

	out, err := container.MergeAuth(existing, "ghcr.io", "alice", "newpass")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := authOf(t, out, "ghcr.io"), base64.StdEncoding.EncodeToString([]byte("alice:newpass")); got != want {
		t.Errorf("auth = %q, want the new credential %q", got, want)
	}

	var doc struct {
		Auths map[string]map[string]any `json:"auths"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	// Exactly one entry for the registry, not a duplicate alongside it.
	if got := len(doc.Auths); got != 1 {
		t.Errorf("auths holds %d registries, want 1: %v", got, doc.Auths)
	}

	// Sibling fields survive, as documented. identitytoken is one of
	// them, and container tools prefer it over auth when both are
	// present -- so a re-login leaves the older credential in effect.
	entry := doc.Auths["ghcr.io"]
	if entry["email"] != "a@b.c" {
		t.Errorf("email dropped: %v", entry["email"])
	}

	if entry["identitytoken"] != "stale-token" {
		t.Errorf("identitytoken = %v; if this is now cleared, update docs/open-questions.md", entry["identitytoken"])
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

func TestRemoveAuth_RemovesTargetPreservesOthers(t *testing.T) {
	t.Parallel()

	existing := []byte(`{"auths":{"ghcr.io":{"auth":"gone"},"codeberg.org":{"auth":"keep"}},"credsStore":"x"}`)

	out, removed, err := container.RemoveAuth(existing, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}

	if !removed {
		t.Fatal("removed = false, want true (ghcr.io was present)")
	}

	if got := authOf(t, out, "ghcr.io"); got != "" {
		t.Errorf("ghcr.io entry not removed: %q", got)
	}

	if got := authOf(t, out, "codeberg.org"); got != "keep" {
		t.Errorf("codeberg.org entry clobbered: %q", got)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	if doc["credsStore"] != "x" {
		t.Errorf("credsStore dropped: %v", doc["credsStore"])
	}
}

func TestRemoveAuth_AbsentEntryAndEmptyAreNoOps(t *testing.T) {
	t.Parallel()

	existing := []byte(`{"auths":{"ghcr.io":{"auth":"keep"}}}`)
	if _, removed, err := container.RemoveAuth(existing, "codeberg.org"); err != nil || removed {
		t.Errorf("absent registry: removed=%v err=%v, want false/nil (idempotent no-op)", removed, err)
	}

	if _, removed, err := container.RemoveAuth(nil, "ghcr.io"); err != nil || removed {
		t.Errorf("empty config: removed=%v err=%v, want false/nil", removed, err)
	}
}

func TestRemoveAuth_RejectsEmptyRegistryAndBadJSON(t *testing.T) {
	t.Parallel()

	if _, _, err := container.RemoveAuth([]byte(`{"auths":{}}`), ""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty registry should be ErrUsage, got %v", err)
	}

	if _, _, err := container.RemoveAuth([]byte("{not json"), "ghcr.io"); !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("bad existing config should be ErrMalformedInput, got %v", err)
	}
}
