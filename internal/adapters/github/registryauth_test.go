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
		return map[string]string{
			"GITHUB_TOKEN":      "ght",
			"GITHUB_ACTOR":      "ci-bot",
			"GITHUB_SERVER_URL": "https://github.com",
		}[k]
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

func TestResolveRegistryAuth_GHESRefusesPublicEndpoint(t *testing.T) {
	t.Parallel()

	tokenRead := false
	p := &github.Provider{Env: func(k string) string {
		if k == "GITHUB_TOKEN" {
			tokenRead = true
		}

		return map[string]string{
			"GITHUB_TOKEN":      "ghes-token",
			"GITHUB_ACTOR":      "ci-bot",
			"GITHUB_SERVER_URL": "https://github.acme.example",
		}[k]
	}}

	if _, err := p.ResolveRegistryAuth(); !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("GHES registry resolution should be ErrUnsupported, got %v", err)
	}

	if tokenRead {
		t.Error("GITHUB_TOKEN was read before GHES registry resolution was refused")
	}
}

func TestResolveRegistryAuth_GitHub_NoToken(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_SERVER_URL": "https://github.com"}[k]
	}}
	if _, err := p.ResolveRegistryAuth(); !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Errorf("missing GITHUB_TOKEN should be ErrCIRuntimeRequired, got %v", err)
	}
}
