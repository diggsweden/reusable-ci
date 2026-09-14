// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
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

	for _, existing := range [][]byte{nil, {}, []byte(" \n\t"), []byte(`{}`)} {
		out, err := container.MergeAuth(existing, "ghcr.io", "alice", "s3cret")
		if err != nil {
			t.Fatal(err)
		}

		assertAuthJSON(t, out, `{"auths":{"ghcr.io":{"auth":"YWxpY2U6czNjcmV0"}}}`)
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
// It also verifies that mutually exclusive authentication forms are not kept
// together: clients prefer identitytoken, so it must not shadow the new auth.
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

	// Unrelated sibling fields survive, while the stale alternate credential is
	// removed so clients use the newly supplied username/password.
	entry := doc.Auths["ghcr.io"]
	if entry["email"] != "a@b.c" {
		t.Errorf("email dropped: %v", entry["email"])
	}

	if _, ok := entry["identitytoken"]; ok {
		t.Errorf("stale identitytoken was retained: %v", entry["identitytoken"])
	}
}

func TestMergeAuth_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		registry string
		username string
		password string
	}{
		{"", "u", "p"},
		{"ghcr.io", "", "p"},
		{"ghcr.io", "u", ""},
	} {
		out, err := container.MergeAuth(nil, tc.registry, tc.username, tc.password)
		if out != nil || !errors.Is(err, errs.ErrUsage) {
			t.Errorf("missing field: out=%q err=%v, want nil/ErrUsage", out, err)
		}
	}
}

func TestRemoveAuth_RemovesTargetPreservesOthers(t *testing.T) {
	t.Parallel()

	existing := []byte(`{"auths":{"ghcr.io":{"auth":"gone"},"codeberg.org":{"auth":"keep","email":"fixture@example.invalid","counter":9007199254740993}},"credsStore":"x"}`)

	out, removed, err := container.RemoveAuth(existing, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}

	if !removed {
		t.Fatal("removed = false, want true (ghcr.io was present)")
	}

	var doc struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	if _, present := doc.Auths["ghcr.io"]; present {
		t.Error("ghcr.io key is still present")
	}

	assertAuthJSON(t, out, `{"auths":{"codeberg.org":{"auth":"keep","counter":9007199254740993,"email":"fixture@example.invalid"}},"credsStore":"x"}`)
}

func TestRemoveAuth_AbsentEntryAndEmptyAreNoOps(t *testing.T) {
	t.Parallel()

	for _, existing := range [][]byte{
		nil, {}, []byte(" \n\t"), []byte(`{}`), []byte(`{"auths":{}}`),
		[]byte(" {\"auths\": {\"ghcr.io\": {\"auth\": \"keep\"}}, \"counter\": 9007199254740993}\n"),
	} {
		out, removed, err := container.RemoveAuth(existing, "codeberg.org")
		if !bytes.Equal(out, existing) || (out == nil) != (existing == nil) || removed || err != nil {
			t.Errorf("no-op: out=%q removed=%v err=%v, want original %q/false/nil", out, removed, err, existing)
		}
	}
}

func TestRemoveAuth_RejectsEmptyRegistry(t *testing.T) {
	t.Parallel()

	out, removed, err := container.RemoveAuth([]byte(`{"auths":{}}`), "")
	if out != nil || removed || !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty registry: out=%q removed=%v err=%v, want nil/false/ErrUsage", out, removed, err)
	}
}

func TestAuth_RejectsNonObjectAndMalformedDocuments(t *testing.T) {
	t.Parallel()

	for _, existing := range []string{"null", "[]", `"text"`, "42", "true", "{not json", `{} {}`, `{} null`, `{} trailing`, "{}\v", "{}\u00a0", "{}\x00"} {
		t.Run(existing, func(t *testing.T) {
			t.Parallel()

			out, err := container.MergeAuth([]byte(existing), "ghcr.io", "u", "p")
			if out != nil || !errors.Is(err, errs.ErrMalformedInput) {
				t.Errorf("merge: out=%q err=%v, want nil/ErrMalformedInput", out, err)
			}

			out, removed, err := container.RemoveAuth([]byte(existing), "ghcr.io")
			if out != nil || removed || !errors.Is(err, errs.ErrMalformedInput) {
				t.Errorf("remove: out=%q removed=%v err=%v, want nil/false/ErrMalformedInput", out, removed, err)
			}
		})
	}
}

func TestAuth_NonObjectNestedValues(t *testing.T) {
	t.Parallel()

	for _, shape := range []string{"null", "[]", `"text"`, "42", "true"} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			// Merge repairs a non-object auths value; removal leaves it untouched.
			existing := []byte(`{"auths":` + shape + `,"counter":9007199254740993}`)

			out, err := container.MergeAuth(existing, "ghcr.io", "u", "p")
			if err != nil {
				t.Fatal(err)
			}

			assertAuthJSON(t, out, `{"auths":{"ghcr.io":{"auth":"dTpw"}},"counter":9007199254740993}`)

			out, removed, err := container.RemoveAuth(existing, "ghcr.io")
			if !bytes.Equal(out, existing) || removed || err != nil {
				t.Errorf("non-object auths: out=%q removed=%v err=%v, want original/false/nil", out, removed, err)
			}

			// A present target is replaced on merge and deleted on removal, whatever its shape.
			existing = []byte(`{"auths":{"codeberg.org":{"auth":"keep"},"ghcr.io":` + shape + `}}`)

			out, err = container.MergeAuth(existing, "ghcr.io", "u", "p")
			if err != nil {
				t.Fatal(err)
			}

			assertAuthJSON(t, out, `{"auths":{"codeberg.org":{"auth":"keep"},"ghcr.io":{"auth":"dTpw"}}}`)

			out, removed, err = container.RemoveAuth(existing, "ghcr.io")
			if err != nil || !removed {
				t.Fatalf("non-object entry: removed=%v err=%v, want true/nil", removed, err)
			}

			assertAuthJSON(t, out, `{"auths":{"codeberg.org":{"auth":"keep"}}}`)
		})
	}
}

func TestAuth_PreservesExactNumbers(t *testing.T) {
	t.Parallel()

	const existing = `{"auths":{"ghcr.io":{"auth":"old","identitytoken":"stale","counter":9007199254740993},"codeberg.org":{"auth":"keep","counter":-9007199254740993}},"counter":9007199254740993,"metadata":[1e400,0.12345678901234567890123456789,null,true]}`

	t.Run("merge", func(t *testing.T) {
		t.Parallel()

		body := []byte(existing)

		out, err := container.MergeAuth(body, "ghcr.io", "u", "p")
		if err != nil {
			t.Fatal(err)
		}

		assertAuthJSON(t, out, `{"auths":{"codeberg.org":{"auth":"keep","counter":-9007199254740993},"ghcr.io":{"auth":"dTpw","counter":9007199254740993}},"counter":9007199254740993,"metadata":[1e400,0.12345678901234567890123456789,null,true]}`)

		if string(body) != existing {
			t.Fatal("merge mutated its input")
		}
	})

	t.Run("remove", func(t *testing.T) {
		t.Parallel()

		body := []byte(existing)

		out, removed, err := container.RemoveAuth(body, "ghcr.io")
		if err != nil || !removed {
			t.Fatalf("remove: removed=%v err=%v, want true/nil", removed, err)
		}

		assertAuthJSON(t, out, `{"auths":{"codeberg.org":{"auth":"keep","counter":-9007199254740993}},"counter":9007199254740993,"metadata":[1e400,0.12345678901234567890123456789,null,true]}`)

		if string(body) != existing {
			t.Fatal("remove mutated its input")
		}
	})
}

func assertAuthJSON(t *testing.T, body []byte, want string) {
	t.Helper()

	// Compact without decoding, so the oracle cannot round numbers through float64.
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatal(err)
	}

	if got := compact.String(); got != want {
		t.Errorf("auth JSON = %s, want %s", got, want)
	}
}
