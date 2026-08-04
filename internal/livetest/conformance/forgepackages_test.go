// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-PKG-*: `publish forge-packages deploy` against the forges' own package
// registries.
//
// An entire command family with no live coverage until now. Both adapters
// implement the resolver roles, and every test underneath this one checks the
// resolved URL and auth scheme against a fake written from the same assumptions
// as the adapter — which is precisely the tie only a real server can break, and
// the registries disagree in ways that make it worth breaking:
//
//   - GitLab deploys to <CI_API_V4_URL>/projects/<id>/packages/... and
//     authenticates with a Job-Token header;
//   - Forgejo deploys to <server>/api/packages/<owner>/... and authenticates
//     with an `Authorization: token` header;
//   - Forgejo requires npm packages to be scoped to the owner; GitLab accepts
//     any name.
//
// On the tier boundary: docs/testing.md keeps toolchain claims in the black-box
// suite, and that rule is respected here. npm is not what is under test — it is
// the transport, the way curl is the transport elsewhere in this tier. The
// claim is a forge-API claim: that what the adapter resolved is what the
// registry accepts. Nothing is asserted about npm's own behaviour, and the
// verdict is read from the forge rather than from the tool that published.
//
// The publish must happen inside a job because GitLab's registry credential is
// $CI_JOB_TOKEN, which exists only while the job runs.

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// packageTokenSecret names the repository secret carrying a package-write
// token. Referenced literally in the Forgejo workflow, so it is declared once
// here and the two must agree.
const packageTokenSecret = "RC_PACKAGE_TOKEN"

// PAR-PKG-1: an npm package published through the product appears in the
// forge's own package registry.
func TestInRunner_ForgePackages_NPMPublishReachesTheRegistry(t *testing.T) {
	const (
		tag  = "v0.0.8-pkgnpm"
		base = "rc-parpkg"
	)

	// Unique per run, and deliberately NOT a prerelease.
	//
	// Unique because the Gitea family scopes packages to the owner, not to the
	// repository: a fresh scratch repo does not give a fresh registry, so a fixed
	// version would collide with the previous run and be refused. Not a
	// prerelease because npm refuses to publish one without an explicit --tag,
	// which would make this scenario fail on npm's release policy rather than on
	// anything the forge did.
	version := fmt.Sprintf("0.0.%d", time.Now().UnixMilli()%1_000_000)

	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "a package registry") {
		if !livetest.RunsInRunner(kind) {
			t.Logf("SKIP %s: the in-runner tier does not drive this forge yet", kind)

			continue
		}

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "pkgnpm")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")
			name := livetest.NPMPackageName(target, base)

			// Forgejo's automatic job token is refused by the package registry
			// (measured: a PUT with it answers 401/403), so the job is given a
			// token that carries write:package — which is what a real Forgejo
			// pipeline has to do too. GitLab needs none: its job token already
			// carries package-write for the project that issued it.
			if kind == provider.PlatformForgejo {
				livetest.SetRepoSecret(t, target, repo, packageTokenSecret, target.Token)
			}

			// The package outlives the scratch repository on the Gitea family,
			// so it is swept explicitly rather than left for the repo delete.
			t.Cleanup(func() {
				livetest.DeletePublishedPackage(t, target, repo, "npm", name, version)
			})

			// A fresh scratch repository each run, so the package cannot already
			// be there — but assert it anyway, because "it was published" and "it
			// was already published" are the same observation afterwards.
			if before := livetest.PublishedPackageVersions(t, target, repo, "npm", name); slices.Contains(before, version) {
				t.Fatalf("%s: %s@%s exists before publishing, so finding it afterwards would prove nothing",
					kind, name, version)
			}

			conclusion := livetest.RunWorkflow(t, target, repo, "forge-packages-npm",
				npmPublishProbe(kind, assetURL, name, version))
			if conclusion != "success" {
				t.Fatalf("%s: the publish job concluded %q — `publish forge-packages deploy --project-type npm` did not complete against this forge",
					kind, conclusion)
			}

			// The forge is the oracle, not npm: the tool that published is the
			// worst possible witness that publishing worked.
			after := livetest.PublishedPackageVersions(t, target, repo, "npm", name)
			if !slices.Contains(after, version) {
				t.Errorf("%s: the job succeeded but %s@%s is not in the forge's package registry (found %v) — the deploy reported success without the package arriving",
					kind, name, version, after)
			}
		})
	}
}

// npmPublishProbe packs a minimal package and publishes it with the product.
//
// The job runs in a node image because the product shells out to npm for this
// ecosystem; that is the product's design, not the scenario's choice. Nothing
// below asserts anything about npm — the assertion is made by the Go test
// against the forge afterwards.
func npmPublishProbe(kind provider.Platform, assetURL, name, version string) string {
	// Single-quoted heredoc: the package manifest must reach disk verbatim,
	// without the shell touching anything inside it.
	//
	// strict-ssl is relaxed for the same reason the prelude fetches with curl -k:
	// a disposable lab serves a locally-trusted CA that the job image's trust
	// store has never heard of. TLS trust is the environment's business, and
	// relaxing it here keeps that accommodation in the fixture rather than
	// tempting a workaround into the product. It is set through the environment
	// because npm ranks env above the --userconfig file the product writes.
	manifest := `export npm_config_strict_ssl=false

mkdir -p pkg
cat > pkg/package.json <<'MANIFEST'
{
  "name": "` + name + `",
  "version": "` + version + `",
  "description": "livetest fixture for PAR-PKG-1",
  "main": "index.js",
  "license": "EUPL-1.2"
}
MANIFEST
echo "module.exports = 1;" > pkg/index.js

# The product publishes the packed tarball, so pack it first.
( cd pkg && npm pack --silent )
ls -l pkg`

	if kind == provider.PlatformGitLab {
		return `publish:
  image: node:24
  script:
    - |
      ` + indent(livetest.ProbePrelude(assetURL), 6) + `
      ` + indent(manifest, 6) + `
      ` + indent(`run_product publish forge-packages deploy --project-type npm --working-dir pkg`, 6) + `
`
	}

	return `on: [push]
jobs:
  publish:
    runs-on: ubuntu-latest
    container:
      image: node:24
    steps:
      - name: publish an npm package to this forge's own registry
        env:
          # Overrides the automatic token, which the package registry refuses.
          FORGEJO_TOKEN: ${{ secrets.RC_PACKAGE_TOKEN }}
        run: |
          ` + indent(livetest.ProbePrelude(assetURL), 10) + `
          ` + indent(manifest, 10) + `
          ` + indent(`run_product publish forge-packages deploy --project-type npm --working-dir pkg`, 10) + `
`
}
