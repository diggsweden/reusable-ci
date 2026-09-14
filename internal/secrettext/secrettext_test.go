// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package secrettext_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
	"github.com/diggsweden/reusable-ci/v3/internal/secrettext"
)

func TestJWTBoundary_JSONSpellingAndBenignControls(t *testing.T) {
	t.Parallel()

	for _, header := range []string{`{"alg":"HS256"}`, `{ "alg": "HS256" }`, "\n{\n \"kid\":\"owned\",\n \"\\u0061lg\":\"HS256\"\n}", `{"alg":"none"}`} {
		unsigned := base64.RawURLEncoding.EncodeToString([]byte(header)) + ".e30"
		mac := hmac.New(sha256.New, []byte("synthetic-test-only-key"))
		_, _ = mac.Write([]byte(unsigned))

		signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if strings.Contains(header, "none") {
			signature = ""
		}

		token := unsigned + "." + signature
		body := []byte("operation failed: " + token)
		before := bytes.Clone(body)

		redacted := safeexec.RedactKeyMaterial(body)
		if !secrettext.ContainsJWT(body) || !container.UnsafeCosignErrorLine(string(body)) || strings.Contains(string(redacted), token) || !strings.Contains(string(redacted), "JWT-shaped") {
			t.Fatalf("missed header spelling %q", header)
		}

		if !bytes.Equal(before, body) {
			t.Fatal("redaction mutated input")
		}
	}

	for _, body := range []string{"release 1.2.3", "reading foo.bar.baz", "YWJj.e30.c2ln", "e30.e30.c2ln"} {
		if secrettext.ContainsJWT([]byte(body)) || container.UnsafeCosignErrorLine(body) || string(safeexec.RedactKeyMaterial([]byte(body))) != body {
			t.Fatalf("benign text redacted: %s", body)
		}
	}
}
