// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestResolveName_WritesNameOutput(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	err := appcontainer.ResolveName(context.Background(), sink, appcontainer.ResolveNameInput{
		Registry:        "ghcr.io", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ImageName:       "",
		Repository:      "owner/repo",
		RepositoryOwner: "owner",
	})
	if err != nil {
		t.Fatalf("ResolveName: %v", err)
	}

	if got := sink.Single("name"); got != "ghcr.io/owner/repo" {
		t.Errorf("Single(name) = %q, want %q", got, "ghcr.io/owner/repo")
	}
}

func TestResolveName_MultiContainerName(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	err := appcontainer.ResolveName(context.Background(), sink, appcontainer.ResolveNameInput{
		Registry:        "ghcr.io",
		ImageName:       "",
		Repository:      "owner/repo",
		RepositoryOwner: "owner",
		Name:            "frontend",
	})
	if err != nil {
		t.Fatalf("ResolveName: %v", err)
	}

	if got := sink.Single("name"); got != "ghcr.io/owner/repo/frontend" {
		t.Errorf("Single(name) = %q, want %q", got, "ghcr.io/owner/repo/frontend")
	}
}
