// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestResolveRegistryAuth_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_TOKEN":      "ftok",
			"FORGEJO_ACTOR":      "bot",
		}[k]
	}}

	auth, err := p.ResolveRegistryAuth()
	if err != nil {
		t.Fatal(err)
	}

	if auth.Username != "bot" || auth.Token != "ftok" {
		t.Errorf("auth = %+v", auth)
	}

	// Registry carries a scheme; MatchesRegistry strips it.
	if !auth.MatchesRegistry("codeberg.org") {
		t.Errorf("MatchesRegistry should accept the forge host, Registry=%q", auth.Registry)
	}
}

func TestResolveRegistryAuth_Forgejo_NoToken(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveRegistryAuth(); !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Errorf("missing token should be ErrCIRuntimeRequired, got %v", err)
	}
}
