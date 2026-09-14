// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// An unresolved hand-walk sees a YAML alias as a leaf with no children:
// the anchored content is never visited. The isolation readers must resolve
// aliases and effective merge values before applying their individual rules.
//
// That made the same workflow pass or fail depending on how it was
// spelled, in the fail-OPEN direction: writing a build job's env behind
// an anchor hid its signing secrets from the release-isolation check.
// The readers now resolve aliases, and the secret walk decodes effective maps.
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

// Checkout identification and credential inspection both follow aliases.
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

func TestCheckIsolation_CheckoutRequiresLiteralFalse(t *testing.T) {
	t.Parallel()

	const workflow = "name: release\non: push\njobs:\n  build:\n    runs-on: ubuntu-24.04\n    steps:\n      - uses: actions/checkout@1111111111111111111111111111111111111111\n"

	for _, tc := range []struct {
		name string
		with string
		want int
	}{
		{"literal_false", "{persist-credentials: false}", 0},
		{"quoted_false", "{persist-credentials: 'false'}", 0},
		{"literal_true", "{persist-credentials: true}", 1},
		{"expression_true", "{persist-credentials: '${{ true }}'}", 1},
		{"expression_false", "{persist-credentials: '${{ false }}'}", 1},
		{"expression_input", "{persist-credentials: '${{ inputs.persist }}'}", 1},
		{"expression_secret", "{persist-credentials: '${{ secrets.COSIGN_PRIVATE_KEY }}'}", 1},
		{"missing_value", "{}", 1},
		{"null_value", "{persist-credentials: null}", 1},
		{"empty_value", "{persist-credentials: ''}", 1},
		{"numeric_value", "{persist-credentials: 0}", 1},
		{"sequence_value", "{persist-credentials: [false]}", 1},
		{"mapping_value", "{persist-credentials: {value: false}}", 1},
		{"unknown_tag", "{persist-credentials: !custom false}", 1},
		{"invalid_boolean_tag", "{persist-credentials: !!int false}", 1},
		{"duplicate_value", "{persist-credentials: false, persist-credentials: true}", 1},
		{"uninspectable_with", "'${{ inputs.checkout }}'", 1},
		{"sequence_with", "[]", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := validate.CheckIsolation([]byte(workflow+"        with: "+tc.with+"\n"), validate.IsolationConfig{})
			if err != nil || len(got) != tc.want {
				t.Fatalf("violations=%v err=%v want %d checkout violations", got, err, tc.want)
			}

			for _, violation := range got {
				if violation.Line != 7 || !strings.Contains(violation.Msg, `job "build"`) || !strings.Contains(violation.Msg, "persist-credentials: false") {
					t.Fatalf("incorrect checkout diagnostic: %v", violation)
				}
			}
		})
	}
}

func TestCheckIsolation_CheckoutAliasPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		value    string
		checkout string
		want     int
	}{
		{"safe_value_and_step_alias", "'false'", "      - *checkout\n", 0},
		{"unsafe_value_and_step_alias", "'${{ true }}'", "      - *checkout\n", 2},
		{"secret_value_and_step_alias", "'${{ secrets.COSIGN_PRIVATE_KEY }}'", "      - *checkout\n", 2},
		{"unsafe_with_alias", "'${{ true }}'", "      - uses: *action\n        with: *credentials\n", 2},
		{"safe_override", "'${{ true }}'", "      - <<: *checkout\n        with:\n          <<: *credentials\n          persist-credentials: false\n", 1},
		{"unsafe_override", "'false'", "      - <<: *checkout\n        with:\n          <<: *credentials\n          persist-credentials: ${{ true }}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := "name: release\non: push\nenv:\n  CHECKOUT_SETTING: &setting " + tc.value + "\njobs:\n  build:\n    runs-on: ubuntu-24.04\n    steps:\n      - &checkout\n        uses: &action actions/checkout@1111111111111111111111111111111111111111\n        with: &credentials\n          persist-credentials: *setting\n" + tc.checkout

			got, err := validate.CheckIsolation([]byte(body), validate.IsolationConfig{})
			if err != nil || len(got) != tc.want {
				t.Fatalf("violations=%v err=%v want %d checkout violations", got, err, tc.want)
			}

			for _, violation := range got {
				if !strings.Contains(violation.Msg, `job "build"`) || !strings.Contains(violation.Msg, "persist-credentials: false") {
					t.Fatalf("incorrect consuming job/rule: %v", violation)
				}
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
		t.Fatalf("unparsable workflow accepted, violations = %v", got)
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

func TestCheckIsolation_SecretMergePrecedence(t *testing.T) {
	t.Parallel()

	const anchors = "x-unsafe: &unsafe\n  KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}\nx-safe: &safe\n  KEY: public\nx-job: &job\n  env: *unsafe\n"

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"direct_safe_override", "jobs:\n  build:\n    env:\n      <<: *unsafe\n      KEY: public\n", 0},
		{"override_before_merge", "jobs:\n  build:\n    env:\n      KEY: public\n      <<: *unsafe\n", 0},
		{"first_merge_wins_safe", "jobs:\n  build:\n    env:\n      <<: [*safe, *unsafe]\n", 0},
		{"first_merge_wins_unsafe", "jobs:\n  build:\n    env:\n      <<: [*unsafe, *safe]\n", 1},
		{"direct_unsafe_override", "jobs:\n  build:\n    env:\n      <<: *safe\n      KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}\n", 1},
		{"unoverridden_secret", "jobs:\n  build:\n    env:\n      <<: *unsafe\n      OTHER: public\n", 1},
		{"whole_job_override", "jobs:\n  build:\n    <<: *job\n    env: *safe\n", 0},
		{"whole_job_inherited", "jobs:\n  build: *job\n", 1},
		{"step_env_override", "jobs:\n  build:\n    steps:\n      - env:\n          <<: *unsafe\n          KEY: public\n", 0},
		{"step_still_unsafe", "jobs:\n  build:\n    env: *safe\n    steps:\n      - env: *unsafe\n", 1},
		{"workflow_merge_override", "env:\n  <<: *unsafe\n  KEY: public\njobs:\n  build:\n    steps: []\n", 0},
		{"workflow_merge_inherited", "env:\n  <<: *unsafe\njobs:\n  build:\n    steps: []\n", 1},
		{"job_merge_overrides_workflow", "env: *unsafe\njobs:\n  build:\n    env:\n      <<: [*safe, *unsafe]\n", 0},
		{"nested_merge_override", "x-nested: &nested\n  <<: *unsafe\n  KEY: public\njobs:\n  build:\n    env:\n      <<: *nested\n", 0},
		{"direct_reference_outside_env", "jobs:\n  build:\n    env:\n      <<: *unsafe\n      KEY: public\n    steps:\n      - run: ${{ secrets.COSIGN_PRIVATE_KEY }}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := checkBuildJob(t, anchors+tc.body)
			if len(got) != tc.want {
				t.Fatalf("violations=%v want %d", got, tc.want)
			}

			for _, violation := range got {
				if !strings.Contains(violation.Msg, `build job "build"`) || !strings.Contains(violation.Msg, `"COSIGN_PRIVATE_KEY"`) {
					t.Fatalf("incorrect consumer/secret: %v", violation)
				}
			}
		})
	}
}

func TestCheckIsolation_RecursiveSecretAliasesRefuse(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"jobs:\n  build: &b\n    steps: *b\n",
		"jobs:\n  build:\n    env: &env\n      <<: *env\n",
		"jobs:\n  build:\n    steps: &steps\n      - env:\n          KEY: *steps\n",
	} {
		got, err := validate.CheckIsolation([]byte(body), validate.IsolationConfig{BuildJob: "build", SigningSecrets: []string{"COSIGN_PRIVATE_KEY"}})
		if err == nil || len(got) != 0 {
			t.Fatalf("recursive secret walk must refuse, not pass/return partial diagnostics: %v %v", got, err)
		}
	}
}

// TestCheckIsolation_WholeContextSecretAccessIsAViolation covers the ways a
// build job reaches every secret without naming one. toJSON(secrets) and a
// computed index read the whole context; `secrets: inherit` hands a reusable
// build job every caller secret. Each used to pass, because only a literal
// `secrets.NAME` was a reference. The direct form stays the control.
func TestCheckIsolation_WholeContextSecretAccessIsAViolation(t *testing.T) {
	t.Parallel()

	cfg := validate.IsolationConfig{BuildJob: "build", SigningSecrets: []string{"RELEASE_GPG_PRIVATE_KEY"}}

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"direct reference": {
			body: "jobs:\n  build:\n    runs-on: x\n    env:\n      KEY: ${{ secrets.RELEASE_GPG_PRIVATE_KEY }}\n    steps: []\n",
			want: `references signing secret "RELEASE_GPG_PRIVATE_KEY"`,
		},
		"toJSON of the context": {
			body: "jobs:\n  build:\n    runs-on: x\n    env:\n      ALL: ${{ toJSON(secrets) }}\n    steps: []\n",
			want: "through the whole secrets context",
		},
		"computed index": {
			body: "jobs:\n  build:\n    runs-on: x\n    env:\n      KEY: ${{ secrets[format('RELEASE_GPG_{0}', 'PRIVATE_KEY')] }}\n    steps: []\n",
			want: "through the whole secrets context",
		},
		"reusable job inheriting secrets": {
			body: "jobs:\n  build:\n    uses: org/repo/.github/workflows/build.yml@0123456789abcdef0123456789abcdef01234567\n    secrets: inherit\n",
			want: "inherits every caller secret",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			violations, err := validate.CheckIsolation([]byte(tc.body), cfg)
			if err != nil {
				t.Fatal(err)
			}

			if len(violations) != 1 || !strings.Contains(violations[0].Msg, tc.want) {
				t.Fatalf("violations = %+v, want one naming %q", violations, tc.want)
			}
		})
	}

	// An unrelated literal secret in the build job is still fine, and a
	// whole-context read is reported once however many secrets are configured.
	clean := "jobs:\n  build:\n    runs-on: x\n    env:\n      TOKEN: ${{ secrets.NPM_TOKEN }}\n    steps: []\n"

	violations, err := validate.CheckIsolation([]byte(clean), cfg)
	if err != nil || len(violations) != 0 {
		t.Fatalf("an unrelated secret was reported: %+v, %v", violations, err)
	}

	two := validate.IsolationConfig{BuildJob: "build", SigningSecrets: []string{"RELEASE_GPG_PRIVATE_KEY", "COSIGN_PRIVATE_KEY"}}
	whole := "jobs:\n  build:\n    runs-on: x\n    env:\n      ALL: ${{ toJSON(secrets) }}\n    steps: []\n"

	if violations, err = validate.CheckIsolation([]byte(whole), two); err != nil || len(violations) != 1 {
		t.Fatalf("a whole-context read was reported %d times: %+v, %v", len(violations), violations, err)
	}
}

// TestCheckIsolation_ActionNamesMatchCaseInsensitively covers the step
// matchers. Forges resolve repository names without regard to case, so
// `Actions/Checkout@` is the same action as `actions/checkout@`; a spelling
// used to slip past the persist-credentials and cache rules.
func TestCheckIsolation_ActionNamesMatchCaseInsensitively(t *testing.T) {
	t.Parallel()

	body := "jobs:\n  build:\n    runs-on: x\n    steps:\n" +
		"      - uses: Actions/Checkout@1111111111111111111111111111111111111111\n" +
		"      - uses: diggsweden/reusable-ci/Setup-Toolchain@1111111111111111111111111111111111111111\n"

	violations, err := validate.CheckIsolation([]byte(body), validate.IsolationConfig{})
	if err != nil {
		t.Fatal(err)
	}

	messages := make([]string, 0, len(violations))
	for _, violation := range violations {
		messages = append(messages, violation.Msg)
	}

	if len(messages) != 2 || !strings.Contains(messages[0], "persist-credentials: false") || !strings.Contains(messages[1], "cache: false") {
		t.Fatalf("violations = %q, want the checkout and setup-toolchain rules", messages)
	}
}

// TestCheckIsolation_ReleaseCallSiteInvariants pins the sign, prepare and
// dist-digest checks that only a survivor showed were unasserted: a prepare
// job's checkout-consumer step orders the secret check like actions/checkout;
// a sign job called from the same repository is refused; bracketed
// needs.<job>.outputs['x'] spellings satisfy the identity check; and a build
// job that does not declare the dist-digest output is refused.
func TestCheckIsolation_ReleaseCallSiteInvariants(t *testing.T) {
	t.Parallel()

	const remote = "org/reusable-ci/.github/workflows/sign.yml@0123456789abcdef0123456789abcdef01234567"

	cfg := validate.IsolationConfig{
		BuildJob: "build", SigningSecrets: []string{"RELEASE_GPG_PRIVATE_KEY"},
		SignJob: "sign", PrepareJob: "prepare", DistDigestOutput: "dist-digest",
	}

	base := func(signUses, prepareSteps, withBlock, buildOutputs string) string {
		return "jobs:\n" +
			"  prepare:\n    runs-on: x\n    outputs:\n      release-tag: v1\n      release-sha: abc\n    steps:\n" + prepareSteps +
			"  build:\n    runs-on: x\n" + buildOutputs + "    steps: []\n" +
			"  sign:\n    uses: " + signUses + "\n    secrets:\n      RELEASE_GPG_PRIVATE_KEY: ${{ secrets.RELEASE_GPG_PRIVATE_KEY }}\n    with:\n" + withBlock
	}

	dotWith := "      release-tag: ${{ needs.prepare.outputs.release-tag }}\n      release-sha: ${{ needs.prepare.outputs.release-sha }}\n      dist-digest: ${{ needs.build.outputs.dist-digest }}\n"
	bracketWith := "      release-tag: ${{ needs.prepare.outputs['release-tag'] }}\n      release-sha: ${{ needs.prepare.outputs[\"release-sha\"] }}\n      dist-digest: ${{ needs.build.outputs['dist-digest'] }}\n"
	safeSteps := "      - run: test -n \"${{ secrets.RELEASE_GPG_PRIVATE_KEY }}\"\n      - uses: org/reusable-ci/checkout-consumer@0123456789abcdef0123456789abcdef01234567\n        with:\n          persist-credentials: false\n"
	lateSteps := "      - uses: org/reusable-ci/checkout-consumer@0123456789abcdef0123456789abcdef01234567\n        with:\n          persist-credentials: false\n      - run: test -n \"${{ secrets.RELEASE_GPG_PRIVATE_KEY }}\"\n"
	digestOutput := "    outputs:\n      dist-digest: ${{ steps.d.outputs.digest }}\n"

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"clean dotted":                   {body: base(remote, safeSteps, dotWith, digestOutput)},
		"clean bracketed":                {body: base(remote, safeSteps, bracketWith, digestOutput)},
		"secret after consumer checkout": {body: base(remote, lateSteps, dotWith, digestOutput), want: "at/after checkout"},
		"local sign job":                 {body: base("./.github/workflows/sign.yml", safeSteps, dotWith, digestOutput), want: "cross-repo workflow_call"},
		"no dist-digest output":          {body: base(remote, safeSteps, dotWith, ""), want: `does not declare "dist-digest" output`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			violations, err := validate.CheckIsolation([]byte(tc.body), cfg)
			if err != nil {
				t.Fatal(err)
			}

			if tc.want == "" {
				if len(violations) != 0 {
					t.Fatalf("violations = %+v, want none", violations)
				}

				return
			}

			if len(violations) != 1 || !strings.Contains(violations[0].Msg, tc.want) {
				t.Fatalf("violations = %+v, want one naming %q", violations, tc.want)
			}
		})
	}
}
