// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// The isolation checker is built two ways. persistCredentialViolations
// decodes each step into a struct, so yaml.v3 resolves aliases and merge
// keys before the check sees the value. Every other check walks
// yaml.Node.Content by hand, and a hand-walk saw an alias node as a leaf
// with no children -- the anchored content was never visited.
//
// That made the same workflow pass or fail depending on how it was
// spelled, in the fail-OPEN direction: writing a build job's env behind
// an anchor hid its signing secrets from the SLSA Build L3 check.
// resolveAlias now closes it in every hand-walk.
//
// Anchors matter here rather than being exotic: this repository's own
// consumer-facing workflow_call files use them for shared `if:`
// conditions, which is also why refusing them outright was not an option.
//
// Each test pairs the anchored spelling with the plain one, so a check
// that stopped resolving fails on the first and a check that stopped
// working entirely fails on both.

const aliasLeakWorkflow = `
x-shared: &sig
  SIGNING_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
jobs:
  build:
    runs-on: ubuntu-latest
    env: *sig
    steps:
      - run: make dist
`

const mergeKeyLeakWorkflow = `
x-shared: &sig
  SIGNING_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      <<: *sig
    steps:
      - run: make dist
`

const directLeakWorkflow = `
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      SIGNING_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
    steps:
      - run: make dist
`

// TestCheckIsolation_BuildJobSecretIsFoundWhenWrittenDirectly is the
// positive control: the check does fire on the plain spelling, so a zero
// count below is about the alias and not about a broken fixture.
func TestCheckIsolation_BuildJobSecretIsFoundWhenWrittenDirectly(t *testing.T) {
	t.Parallel()

	got := checkBuildJob(t, directLeakWorkflow)
	if len(got) != 1 {
		t.Fatalf("violations = %d, want 1: %v", len(got), got)
	}

	if !strings.Contains(got[0].Msg, "COSIGN_PRIVATE_KEY") {
		t.Errorf("violation does not name the secret: %q", got[0].Msg)
	}
}

// TestCheckIsolation_YAMLAliasDoesNotHideTheSigningSecret covers both
// anchored spellings a workflow can use to reach the same value.
func TestCheckIsolation_YAMLAliasDoesNotHideTheSigningSecret(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "alias as the whole value", body: aliasLeakWorkflow},
		{name: "merge key", body: mergeKeyLeakWorkflow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := checkBuildJob(t, tc.body)
			if len(got) != 1 {
				t.Fatalf("violations = %v, want the signing secret reported through the alias", got)
			}

			if !strings.Contains(got[0].Msg, "COSIGN_PRIVATE_KEY") {
				t.Errorf("violation does not name the secret: %q", got[0].Msg)
			}
		})
	}
}

// TestCheckIsolation_PersistCredentialsSeesThroughAliases is the other
// half of the same boundary: this check decodes rather than walks, so the
// anchored spelling is caught. Kept next to its sibling because the
// contrast is the finding.
func TestCheckIsolation_PersistCredentialsSeesThroughAliases(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "written directly",
			body: `
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
        with:
          persist-credentials: true
`,
		},
		{
			name: "with: block behind an alias",
			body: `
x-with: &w
  persist-credentials: true
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
        with: *w
`,
		},
		{
			name: "whole step behind an alias",
			body: `
x-step: &s
  uses: actions/checkout@v4
jobs:
  build:
    steps:
      - *s
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := validate.CheckIsolation([]byte(tc.body), validate.IsolationConfig{})
			if err != nil {
				t.Fatal(err)
			}

			if len(got) != 1 || !strings.Contains(got[0].Msg, "persist-credentials") {
				t.Fatalf("violations = %v, want one persist-credentials violation", got)
			}
		})
	}
}

// TestCheckIsolation_AliasedSetupToolchainStillMeetsTheCacheRule is the
// same resolution reached through a different check: in release isolation
// mode setup-toolchain must run with cache: false, and an anchored step
// is examined like any other.
func TestCheckIsolation_AliasedSetupToolchainStillMeetsTheCacheRule(t *testing.T) {
	t.Parallel()

	const direct = `
jobs:
  build:
    steps:
      - uses: itiquette/forgejo-ci/setup-toolchain@1111111111111111111111111111111111111111
        with:
          cache: true
`

	got, err := validate.CheckIsolation([]byte(direct), validate.IsolationConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !strings.Contains(got[0].Msg, "cache: false") {
		t.Fatalf("positive control: violations = %v, want one cache violation", got)
	}

	const aliased = `
x-step: &t
  uses: itiquette/forgejo-ci/setup-toolchain@1111111111111111111111111111111111111111
  with:
    cache: true
jobs:
  build:
    steps:
      - *t
`

	got, err = validate.CheckIsolation([]byte(aliased), validate.IsolationConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !strings.Contains(got[0].Msg, "cache: false") {
		t.Fatalf("violations = %v, want the cache rule reported through the alias", got)
	}
}

// TestCheckIsolation_MalformedYAMLIsAnError keeps the checker from
// reporting "no violations" for a file it could not read.
func TestCheckIsolation_MalformedYAMLIsAnError(t *testing.T) {
	t.Parallel()

	got, err := validate.CheckIsolation([]byte("jobs:\n  build:\n   - ]["), validate.IsolationConfig{BuildJob: "build"})
	if err == nil {
		t.Fatalf("unparseable workflow accepted, violations = %v", got)
	}
}

// TestCheckIsolation_MissingBuildJobIsAViolation covers the fail-closed
// direction: a renamed or removed build job must not read as "the build
// job holds no signing secret".
func TestCheckIsolation_MissingBuildJobIsAViolation(t *testing.T) {
	t.Parallel()

	got := checkBuildJob(t, "jobs:\n  compile:\n    steps:\n      - run: make\n")
	if len(got) != 1 || !strings.Contains(got[0].Msg, "missing from workflow") {
		t.Fatalf("violations = %v, want one missing-build-job violation", got)
	}
}

func checkBuildJob(t *testing.T, body string) []validate.IsolationViolation {
	t.Helper()

	got, err := validate.CheckIsolation([]byte(body), validate.IsolationConfig{
		BuildJob:       "build",
		SigningSecrets: []string{"COSIGN_PRIVATE_KEY", "RELEASE_GPG_PRIVATE_KEY"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// TestCheckIsolation_MergeKeyPrecedence covers the one place alias
// resolution can go wrong quietly. A key written directly on the mapping
// must win over one pulled in by `<<`, per the merge-key spec -- reading
// the merged value instead would report a workflow's shared default
// rather than what the job actually sets.
func TestCheckIsolation_MergeKeyPrecedence(t *testing.T) {
	t.Parallel()

	// The shared block sets cache: false; this step overrides it to true.
	// The override is the truth, and it is the violation.
	const overridden = `
x-with: &shared
  cache: false
jobs:
  build:
    steps:
      - uses: itiquette/forgejo-ci/setup-toolchain@1111111111111111111111111111111111111111
        with:
          <<: *shared
          cache: true
`

	got, err := validate.CheckIsolation([]byte(overridden), validate.IsolationConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !strings.Contains(got[0].Msg, "cache: false") {
		t.Fatalf("violations = %v, want the direct cache: true to win over the merged default", got)
	}

	// And the reverse: the merged value is used when the job sets nothing.
	const inherited = `
x-with: &shared
  cache: false
jobs:
  build:
    steps:
      - uses: itiquette/forgejo-ci/setup-toolchain@1111111111111111111111111111111111111111
        with:
          <<: *shared
`

	got, err = validate.CheckIsolation([]byte(inherited), validate.IsolationConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf("violations = %v, want none -- the merged cache: false satisfies the rule", got)
	}
}

// TestCheckIsolation_SelfReferentialAliasTerminates covers a workflow
// that anchors a job and aliases it from inside itself.
//
// Honest about what it shows: this terminates because yaml.v3 resolves
// the alias to the mapping in one hop, not because aliasDepthLimit is
// reached -- poisoning the limit away does not fail this test. A chain
// of aliases long enough to need the bound is not expressible in YAML
// the parser accepts, so the limit is belt-and-braces against a
// hand-built node tree rather than something a workflow can reach. It
// stays because the walk runs over caller-supplied files and the cost
// is one comparison.
func TestCheckIsolation_SelfReferentialAliasTerminates(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = validate.CheckIsolation([]byte("jobs:\n  build: &b\n    steps: *b\n"), validate.IsolationConfig{BuildJob: "build"})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("alias resolution did not terminate")
	}
}
