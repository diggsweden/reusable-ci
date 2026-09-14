// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

// RegistryAuth carries a live registry credential — the runner's own token,
// good for pushing images to the forge's registry — and is handed between
// adapters. That means it reaches the places values get formatted by accident:
// a wrapped error, a debug line, a %+v on some struct that embeds it.
//
// runcontext.Credential has redacted formatting for exactly this reason. This
// type held the same class of secret in a plain string field and had none, so
// a single %v anywhere in the login path would have put a registry token in the
// build log.

var errLoginRefused = errors.New("registry login refused") //nolint:err113 // fixture cause.

//nolint:gosec // G101: a deliberately recognisable fixture value, not a credential.
const registryTokenCanary = "glpat-OWNED-SYNTHETIC-CANARY"

func TestRegistryAuth_FormattingNeverPrintsTheToken(t *testing.T) {
	t.Parallel()

	auth := provider.RegistryAuth{
		Registry: "registry.gitlab.com",
		Username: "gitlab-ci-token",
		Token:    registryTokenCanary,
	}

	for _, tc := range []struct {
		name   string
		render string
		why    string
	}{
		{name: "%v", render: fmt.Sprintf("%v", auth), why: "the default verb, reached by any log or error that includes the value"},
		{name: "String()", render: auth.String(), why: "the method every string verb routes through"},
		{name: "%+v", render: fmt.Sprintf("%+v", auth), why: "the field dump a developer reaches for while debugging"},
		{name: "%#v", render: fmt.Sprintf("%#v", auth), why: "the Go-syntax dump"},
		{name: "%q", render: fmt.Sprintf("%q", auth), why: "quoted, which routes through String too"},
		{name: "inside an error", render: fmt.Errorf("login failed: %w: auth %v", errLoginRefused, auth).Error(),
			why: "the shape this actually reaches a log in"},
		{name: "inside a slice", render: fmt.Sprintf("%v", []provider.RegistryAuth{auth}),
			why: "a container's formatting delegates to the element"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NotContainsf(t, tc.render, registryTokenCanary,
				"%s printed the registry token: %s\n%s", tc.name, tc.render, tc.why)
			require.Containsf(t, tc.render, "REDACTED",
				"%s neither printed nor redacted the token, so the field may have been dropped instead: %s",
				tc.name, tc.render)
		})
	}
}

// Redaction must not cost the diagnostic its usefulness. A login failure has to
// say which registry and which username were used, or the redaction is the kind
// that gets reverted the first time someone debugs a real failure.
func TestRegistryAuth_FormattingKeepsTheNonSecretFields(t *testing.T) {
	t.Parallel()

	rendered := fmt.Sprintf("%v", provider.RegistryAuth{
		Registry: "registry.gitlab.com",
		Username: "gitlab-ci-token",
		Token:    registryTokenCanary,
	})

	require.Contains(t, rendered, "registry.gitlab.com", "the diagnostic cannot say which registry was used")
	require.Contains(t, rendered, "gitlab-ci-token", "the diagnostic cannot say which identity was used")
}

// An absent token and a redacted one are different facts. A caller debugging a
// login that failed because no credential was resolved needs to tell them apart.
func TestRegistryAuth_AnAbsentTokenIsDistinguishableFromARedactedOne(t *testing.T) {
	t.Parallel()

	empty := fmt.Sprintf("%v", provider.RegistryAuth{Registry: "ghcr.io", Username: "x"})
	present := fmt.Sprintf("%v", provider.RegistryAuth{Registry: "ghcr.io", Username: "x", Token: registryTokenCanary})

	require.Contains(t, empty, "absent")
	require.NotContains(t, empty, "REDACTED")
	require.Contains(t, present, "REDACTED")
	require.NotEqual(t, empty, present, "an unresolved credential renders identically to a resolved one")
}
