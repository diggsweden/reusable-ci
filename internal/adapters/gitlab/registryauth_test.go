// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestResolveRegistryAuth_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"CI_REGISTRY":          "registry.gitlab.com",
			"CI_REGISTRY_PASSWORD": "jobtok",
		}[k]
	}}

	auth, err := p.ResolveRegistryAuth()
	if err != nil {
		t.Fatal(err)
	}

	// CI_REGISTRY_USER unset → defaults to gitlab-ci-token.
	if auth.Registry != "registry.gitlab.com" || auth.Username != "gitlab-ci-token" || auth.Token != "jobtok" {
		t.Errorf("auth = %+v", auth)
	}
}

func TestResolveRegistryAuth_GitLab_NoCreds(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveRegistryAuth(); !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Errorf("missing registry creds should be ErrCIRuntimeRequired, got %v", err)
	}
}
