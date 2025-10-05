// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestBuildRequest_Validate(t *testing.T) {
	t.Parallel()

	base := container.BuildRequest{Context: ".", Mode: container.BuildModeLoad, ImageRef: "img:tag"}

	t.Run("valid load", func(t *testing.T) {
		t.Parallel()

		if err := base.Validate(); err != nil {
			t.Fatalf("valid load request errored: %v", err)
		}
	})

	cases := map[string]container.BuildRequest{
		"empty context":            {Mode: container.BuildModeLoad, ImageRef: "x"},
		"load without image ref":   {Context: ".", Mode: container.BuildModeLoad},
		"push without image ref":   {Context: ".", Mode: container.BuildModePushByDigest},
		"local without output dir": {Context: ".", Mode: container.BuildModeLocal},
		"unknown mode":             {Context: ".", Mode: "bogus", ImageRef: "x"},
		"malformed secret":         {Context: ".", Mode: container.BuildModeLoad, ImageRef: "x", Secrets: []string{"src=/p"}},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := req.Validate(); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("%s: got %v, want ErrUsage", name, err)
			}
		})
	}

	t.Run("well-formed secret passes", func(t *testing.T) {
		t.Parallel()

		req := base
		req.Secrets = []string{"id=db,src=/run/secrets/db"}

		if err := req.Validate(); err != nil {
			t.Errorf("well-formed secret rejected: %v", err)
		}
	})
}

func TestBuildRequest_CacheRef(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		repo, scope, want string
	}{
		"repo and scope": {"ghcr.io/o/buildcache", "img-amd64", "ghcr.io/o/buildcache:img-amd64"},
		"repo only":      {"ghcr.io/o/buildcache", "", "ghcr.io/o/buildcache"},
		"no repo":        {"", "img-amd64", ""},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := container.BuildRequest{CacheRepo: c.repo, CacheScope: c.scope}
			if got := req.CacheRef(); got != c.want {
				t.Errorf("CacheRef = %q, want %q", got, c.want)
			}
		})
	}
}
