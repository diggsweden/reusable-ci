// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// The isolation checker is built two ways. persistCredentialViolations
// decodes each step into a struct, which makes yaml.v3 resolve aliases and
// merge keys before the check ever sees the value. Every other check walks
// yaml.Node.Content by hand, and a hand-walk sees an alias node as a leaf
// with no children -- the anchored content is never visited.
//
// The consequence is that the same workflow passes or fails depending on
// how it is spelled, and the direction is fail-OPEN: writing the secret
// through an anchor makes the SLSA Build L3 check report nothing. These
// tests pin both halves, with a positive control alongside each, so the
// pair is what fails when the walk learns to follow aliases.
//
// Recorded as an open question in docs/open-questions.md ("A YAML alias
// hides a signing secret from the build-job isolation check"); the tests
// here document today's behaviour rather than assert it is right.

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

// TestCheckIsolation_YAMLAliasHidesTheSigningSecret records the gap. If
// this test starts failing, the hand-walk learned to follow aliases:
// delete the test and close the open question.
func TestCheckIsolation_YAMLAliasHidesTheSigningSecret(t *testing.T) {
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

			if got := checkBuildJob(t, tc.body); len(got) != 0 {
				t.Fatalf("the alias blindness is fixed -- violations = %v.\n"+
					"Close the open question and delete this test.", got)
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

// TestCheckIsolation_AliasedSetupToolchainEscapesTheCacheRule is the same
// blindness reached through a different check: in release isolation mode
// setup-toolchain must run with cache: false, and an anchored step is not
// examined at all -- so a cached toolchain passes.
func TestCheckIsolation_AliasedSetupToolchainEscapesTheCacheRule(t *testing.T) {
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

	if len(got) != 0 {
		t.Fatalf("the alias blindness is fixed -- violations = %v.\n"+
			"Close the open question and delete this test.", got)
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
