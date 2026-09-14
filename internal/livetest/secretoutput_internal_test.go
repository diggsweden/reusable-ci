// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestTargetSecretOutput_FlagsAndRedactsEveryCredentialForm plants each form of
// a synthetic credential in captured output: the token, its URL-escaped form,
// and the base64 forms of the Basic pair and of the token. Each is flagged
// without the diagnostic printing it, and redaction leaves no trace of it
// while keeping the non-secret text around it, including the username alone.
func TestTargetSecretOutput_FlagsAndRedactsEveryCredentialForm(t *testing.T) {
	t.Parallel()

	//nolint:gosec // A synthetic credential shaped to exercise every escaped and encoded form.
	target := Target{Forge: provider.ForgeGitLab, Host: "gitlab.compose.forgelab:8443", Owner: "fixture-user", CredentialUsername: "fixture-user", Token: "glpat-synth/etic+token="}
	pair := target.CredentialUsername + ":" + target.Token

	for name, leaked := range map[string]string{ //nolint:gosec // Encoded forms of the synthetic credential.
		"token":                  target.Token,
		"query-escaped token":    "glpat-synth%2Fetic%2Btoken%3D",
		"path-escaped token":     "glpat-synth%2Fetic+token=",
		"Basic pair":             "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(pair)),
		"unpadded Basic pair":    base64.RawStdEncoding.EncodeToString([]byte(pair)),
		"URL-safe Basic pair":    base64.URLEncoding.EncodeToString([]byte(pair)),
		"auth file token base64": base64.StdEncoding.EncodeToString([]byte(target.Token)),
	} {
		output := "before " + leaked + " after owner fixture-user"

		tb := &recordingTB{}
		assertNoTargetSecret(tb, target, "stderr of release sign", output)

		if len(tb.errors) != 1 || !strings.Contains(tb.errors[0], "stderr of release sign") || strings.Contains(tb.errors[0], leaked) {
			t.Errorf("%s: diagnostics = %q, want one error naming the stream and not the value", name, tb.errors)
		}

		redacted := redactTargetSecrets(target, output)
		for _, form := range targetSecretForms(target) {
			if strings.Contains(redacted, form) {
				t.Errorf("%s: redacted output still holds a credential form: %q", name, redacted)
			}
		}

		if !strings.HasPrefix(redacted, "before ") || !strings.HasSuffix(redacted, " after owner fixture-user") {
			t.Errorf("%s: redaction removed non-secret text: %q", name, redacted)
		}
	}

	clean := &recordingTB{}
	assertNoTargetSecret(clean, target, "stdout", "owner fixture-user pushed gitlab.compose.forgelab:8443/fixture-user/app")

	if len(clean.errors) != 0 {
		t.Errorf("output without the credential was flagged: %q", clean.errors)
	}

	if forms := targetSecretForms(Target{CredentialUsername: "fixture-user"}); forms != nil {
		t.Errorf("a target without a token has secret forms %q, which would redact the username", forms)
	}
}
