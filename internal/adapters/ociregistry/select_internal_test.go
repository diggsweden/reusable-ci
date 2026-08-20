// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"errors"
	"testing"

	"github.com/docker/cli/cli/config/configfile"
	dockertypes "github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// findLinuxArchDescriptor picks which manifest out of a multi-platform
// index the caller then inspects, signs or promotes. Picking the wrong
// child means attesting to an image nobody built for that platform, so
// both refusals matter as much as the match: an ambiguous index and an
// index with no matching entry are errors, not a best guess.
func TestFindLinuxArchDescriptor(t *testing.T) {
	t.Parallel()

	desc := func(arch, os, digest string) v1.Descriptor {
		return v1.Descriptor{
			Digest:   v1.Hash{Algorithm: "sha256", Hex: digest},
			Platform: &v1.Platform{Architecture: arch, OS: os},
		}
	}

	const (
		amdDigest = "1111111111111111111111111111111111111111111111111111111111111111"
		armDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	)

	for _, tc := range []struct {
		name       string
		manifests  []v1.Descriptor
		arch       string
		wantDigest string
		wantErr    error
	}{
		{
			name:       "picks the entry for the requested architecture",
			manifests:  []v1.Descriptor{desc("amd64", "linux", amdDigest), desc("arm64", "linux", armDigest)},
			arch:       "arm64",
			wantDigest: armDigest,
		},
		{
			// BuildKit adds provenance and SBOM manifests as
			// unknown/unknown children. Selecting one of those would
			// inspect an attestation as though it were the image.
			name: "ignores BuildKit attestation manifests",
			manifests: []v1.Descriptor{
				desc("unknown", "unknown", "3333333333333333333333333333333333333333333333333333333333333333"),
				desc("amd64", "linux", amdDigest),
				desc("unknown", "unknown", "4444444444444444444444444444444444444444444444444444444444444444"),
			},
			arch:       "amd64",
			wantDigest: amdDigest,
		},
		{
			// A same-arch entry for another OS is a different image.
			name:      "a windows entry does not satisfy a linux request",
			manifests: []v1.Descriptor{desc("amd64", "windows", amdDigest)},
			arch:      "amd64",
			wantErr:   errs.ErrMissingInput,
		},
		{
			name:      "no entry for the architecture",
			manifests: []v1.Descriptor{desc("amd64", "linux", amdDigest)},
			arch:      "s390x",
			wantErr:   errs.ErrMissingInput,
		},
		{
			// Two candidates and no rule to choose between them: an
			// index like this must be reported, not silently resolved
			// to whichever came first.
			name:      "an ambiguous index is refused",
			manifests: []v1.Descriptor{desc("amd64", "linux", amdDigest), desc("amd64", "linux", armDigest)},
			arch:      "amd64",
			wantErr:   errs.ErrValidation,
		},
		{
			name:      "an empty index",
			manifests: nil,
			arch:      "amd64",
			wantErr:   errs.ErrMissingInput,
		},
		{
			// A child with no platform at all carries no claim about
			// what it is, so it cannot answer an architecture request.
			name:      "a child without platform information",
			manifests: []v1.Descriptor{{Digest: v1.Hash{Algorithm: "sha256", Hex: amdDigest}}},
			arch:      "amd64",
			wantErr:   errs.ErrMissingInput,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := findLinuxArchDescriptor(&v1.IndexManifest{Manifests: tc.manifests}, "registry/img:tag", tc.arch)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Digest.Hex != tc.wantDigest {
				t.Errorf("digest = %s, want %s", got.Digest.Hex, tc.wantDigest)
			}
		})
	}
}

// TestDockerAuthConfig covers which credential is handed to a registry.
// The lookup tries the repository-scoped key before the registry-wide
// one, so a token issued for one repository is used there rather than
// the broader credential; getting the order wrong sends the wrong
// credential to a real registry.
func TestDockerAuthConfig(t *testing.T) {
	t.Parallel()

	repoRef, err := name.NewRepository("ghcr.io/owner/project")
	if err != nil {
		t.Fatal(err)
	}

	cf := &configfile.ConfigFile{AuthConfigs: map[string]dockertypes.AuthConfig{
		"ghcr.io/owner/project": {Username: "scoped", Password: "scoped-secret", ServerAddress: "ghcr.io"},
		"ghcr.io":               {Username: "registry-wide", Password: "wide-secret"},
	}}

	got, err := dockerAuthConfig(cf, repoRef)
	if err != nil {
		t.Fatal(err)
	}

	if got.Username != "scoped" {
		t.Errorf("username = %q, want the repository-scoped credential", got.Username)
	}

	// ServerAddress is cleared before the credential travels on: it is
	// not a credential field, and leaving it set makes two otherwise
	// equal configs compare unequal against the empty value below.
	if got.ServerAddress != "" {
		t.Errorf("ServerAddress = %q, want it cleared", got.ServerAddress)
	}
}

// TestDockerAuthConfig_FallsBackToTheRegistryKey covers the common
// shape: one credential for the whole registry and none per repository.
func TestDockerAuthConfig_FallsBackToTheRegistryKey(t *testing.T) {
	t.Parallel()

	repoRef, err := name.NewRepository("ghcr.io/owner/project")
	if err != nil {
		t.Fatal(err)
	}

	cf := &configfile.ConfigFile{AuthConfigs: map[string]dockertypes.AuthConfig{
		"ghcr.io": {Username: "registry-wide", Password: "wide-secret"},
	}}

	got, err := dockerAuthConfig(cf, repoRef)
	if err != nil {
		t.Fatal(err)
	}

	if got.Username != "registry-wide" {
		t.Errorf("username = %q, want the registry-wide credential", got.Username)
	}
}

// TestDockerAuthConfig_UnknownRegistryGetsNothing is the refusal that
// matters: a registry with no entry must come back empty, which the
// keychain turns into an anonymous request. Returning some other
// registry's credential would send it over the wire.
func TestDockerAuthConfig_UnknownRegistryGetsNothing(t *testing.T) {
	t.Parallel()

	repoRef, err := name.NewRepository("registry.example.com/owner/project")
	if err != nil {
		t.Fatal(err)
	}

	cf := &configfile.ConfigFile{AuthConfigs: map[string]dockertypes.AuthConfig{
		"ghcr.io": {Username: "registry-wide", Password: "wide-secret"},
	}}

	got, err := dockerAuthConfig(cf, repoRef)
	if err != nil {
		t.Fatal(err)
	}

	var empty dockertypes.AuthConfig
	if got != empty {
		t.Errorf("credential for an unconfigured registry = %+v, want none", got)
	}
}

// TestDockerAuthConfig_DockerHubCredentialResolves covers the one
// registry whose config key is not its hostname: Docker Hub credentials
// live under https://index.docker.io/v1/.
//
// It pins the outcome, not the mechanism. dockerAuthConfig maps
// name.DefaultRegistry to authn.DefaultAuthKey itself, but docker/cli's
// own GetAuthConfig already resolves "index.docker.io" to the legacy
// key, so removing that mapping does not change this result -- verified
// by poisoning. The mapping is redundant belt-and-braces rather than
// load-bearing, and this test deliberately does not claim otherwise.
func TestDockerAuthConfig_DockerHubCredentialResolves(t *testing.T) {
	t.Parallel()

	repoRef, err := name.NewRepository("library/alpine")
	if err != nil {
		t.Fatal(err)
	}

	if repoRef.RegistryStr() != name.DefaultRegistry {
		t.Fatalf("fixture does not resolve to the default registry: %s", repoRef.RegistryStr())
	}

	cf := &configfile.ConfigFile{AuthConfigs: map[string]dockertypes.AuthConfig{
		authn.DefaultAuthKey: {Username: "hub-user", Password: "hub-secret"},
	}}

	got, err := dockerAuthConfig(cf, repoRef)
	if err != nil {
		t.Fatal(err)
	}

	if got.Username != "hub-user" {
		t.Errorf("username = %q, want the Docker Hub credential stored under %s", got.Username, authn.DefaultAuthKey)
	}
}
