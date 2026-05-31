// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestResolveForgeMavenRegistry_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{"FORGEJO_SERVER_URL": "https://codeberg.org", "FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_TOKEN": "ft"}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://codeberg.org/api/packages/owner/maven" || reg.AuthScheme != provider.MavenAuthTokenHeader || reg.Token != "ft" {
		t.Errorf("registry = %+v", reg)
	}
}

func TestResolveForgeNPMRegistry_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{"FORGEJO_SERVER_URL": "https://codeberg.org", "FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_TOKEN": "ft"}[k]
	}}

	reg, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Registry != "https://codeberg.org/api/packages/owner/npm/" || reg.Scope != "@owner" {
		t.Errorf("npm registry = %+v", reg)
	}
}
