// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestResolveRegistryAuth_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_TOKEN": "ght", "GITHUB_ACTOR": "ci-bot"}[k]
	}}

	auth, err := p.ResolveRegistryAuth()
	if err != nil {
		t.Fatal(err)
	}

	if auth.Registry != "ghcr.io" || auth.Username != "ci-bot" || auth.Token != "ght" {
		t.Errorf("auth = %+v", auth)
	}

	if !auth.MatchesRegistry("ghcr.io") || auth.MatchesRegistry("docker.io") {
		t.Error("MatchesRegistry should accept ghcr.io and reject docker.io")
	}
}

func TestResolveRegistryAuth_GitHub_NoToken(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveRegistryAuth(); !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Errorf("missing GITHUB_TOKEN should be ErrRuntimeRequired, got %v", err)
	}
}
