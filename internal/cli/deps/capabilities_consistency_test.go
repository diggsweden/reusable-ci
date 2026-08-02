// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package deps_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestCapabilitiesMatchImplementedRoles closes the dual-encoding gap between
// provider.Capabilities (the informational bools `doctor` reports) and the
// role interfaces that actually gate execution via requireRole. For every
// capability backed by a role interface, the advertised bool must equal
// whether the adapter implements that role — otherwise doctor would promise a
// feature the command layer refuses, or hide one it supports.
//
// Scope is the role-backed capabilities. KeylessOIDC is already single-sourced
// (each adapter's SupportsKeyless delegates to Capabilities().KeylessOIDC) and
// Attestation has no role interface, so neither can drift and both are out of
// scope here.
func TestCapabilitiesMatchImplementedRoles(t *testing.T) {
	t.Parallel()

	providers := []struct {
		name string
		p    interface {
			Capabilities() provider.Capabilities
		}
	}{
		{"github", github.New()},
		{"forgejo", forgejo.New()},
		{"gitlab", gitlab.New()},
	}

	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caps := tc.p.Capabilities()

			if _, ok := tc.p.(provider.SARIFUploader); ok != caps.SARIFUpload {
				t.Errorf("SARIFUpload=%v but implements SARIFUploader=%v", caps.SARIFUpload, ok)
			}

			if _, ok := tc.p.(provider.ReleaseAssetUploader); ok != caps.ReleaseAssets {
				t.Errorf("ReleaseAssets=%v but implements ReleaseAssetUploader=%v", caps.ReleaseAssets, ok)
			}

			// The run-artifact store is a matched pair: a provider implements
			// both roles (capability true) or neither (false), never half.
			_, upl := tc.p.(provider.RunArtifactUploader)
			_, dl := tc.p.(provider.RunArtifactDownloader)
			if upl != caps.RunArtifacts || dl != caps.RunArtifacts {
				t.Errorf("RunArtifacts=%v but implements Uploader=%v Downloader=%v",
					caps.RunArtifacts, upl, dl)
			}
		})
	}
}
