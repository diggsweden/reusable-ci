// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestResolveKeylessIdentity_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"CI_PROJECT_URL": "https://gitlab.com/grp/sub/proj",
			"CI_SERVER_URL":  "https://gitlab.com",
		}[k]
	}}

	if !p.SupportsKeyless() {
		t.Fatal("GitLab should support keyless")
	}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if id.OIDCIssuer != "https://gitlab.com" {
		t.Errorf("OIDCIssuer = %q", id.OIDCIssuer)
	}

	if want := `^https://gitlab\.com/grp/sub/proj/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q", id.SubjectRegexp, want)
	}
}

func TestResolveKeylessIdentity_GitLab_MissingProjectURL(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing CI_PROJECT_URL should be ErrUsage, got %v", err)
	}
}
