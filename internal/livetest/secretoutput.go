// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"cmp"
	"encoding/base64"
	"net/url"
	"slices"
	"strings"
)

const redactedTargetSecret = "[REDACTED-TARGET-CREDENTIAL]"

// targetSecretForms returns every spelling of the target's credential that
// captured output must never contain: the token (it is also the registry
// password), its URL-escaped form, and the base64 forms a client derives for
// Basic authorization or an auth file. The username alone is not secret -- it
// is usually the owner every URL names -- so it appears only inside those
// derived forms.
func targetSecretForms(target Target) []string {
	return SecretForms(target.CredentialUsername, target.Token)
}

// SecretForms is targetSecretForms for any credential a scenario handles
// itself, such as a deliberately refused token that no Target carries.
func SecretForms(username, secret string) []string {
	if secret == "" {
		return nil
	}

	pair := []byte(username + ":" + secret)
	token := []byte(secret)

	forms := []string{
		secret,
		url.QueryEscape(secret),
		url.PathEscape(secret),
		base64.StdEncoding.EncodeToString(pair),
		base64.RawStdEncoding.EncodeToString(pair),
		base64.URLEncoding.EncodeToString(pair),
		base64.RawURLEncoding.EncodeToString(pair),
		base64.StdEncoding.EncodeToString(token),
		base64.RawStdEncoding.EncodeToString(token),
	}

	// Longest first, so a derived form is replaced before a shorter form it
	// contains could leave it half-replaced.
	slices.SortFunc(forms, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b))
	})

	return slices.Compact(forms)
}

// redactTargetSecrets replaces every credential form in text.
func redactTargetSecrets(target Target, text string) string {
	for _, form := range targetSecretForms(target) {
		text = strings.ReplaceAll(text, form, redactedTargetSecret)
	}

	return text
}

// assertNoTargetSecret fails the test when captured output carries the target's
// credential in any known form. It names where the credential appeared and
// never prints it.
func assertNoTargetSecret(tb TB, target Target, source, text string) {
	tb.Helper()

	if redactTargetSecrets(target, text) != text {
		tb.Errorf("livetest: %s for %s contains the target credential; the product or probe leaked it", source, target)
	}
}
