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
