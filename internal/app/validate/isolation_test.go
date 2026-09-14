// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const isolationCleanWorkflow = `name: release
jobs:
  build:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - run: make dist
  sign:
    needs: build
    uses: ./.github/workflows/sign.yml
    secrets:
      RELEASE_GPG_PRIVATE_KEY: ${{ secrets.RELEASE_GPG_PRIVATE_KEY }}
`

func runIsolation(t *testing.T, body string) (string, error) {
	t.Helper()

	mem := testfs.NewMemory(t)
	mem.WriteFile(".github/workflows/release.yml", []byte(body))

	var out bytes.Buffer

	err := appvalidate.Isolation(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.IsolationInput{
		Workflow:       ".github/workflows/release.yml",
		BuildJob:       "build",
		SigningSecrets: []string{"RELEASE_GPG_PRIVATE_KEY", "COSIGN_PRIVATE_KEY"},
		FS:             mem.FS(),
	})

	return out.String(), err
}

// forgejoIsolation runs the forgejo-shaped gate. The three wrappers below
// vary exactly one field each; spelling the whole IsolationInput out once per
// wrapper meant a change to the shape had to be made in three places.
func forgejoIsolation(t *testing.T, mem *testfs.Memory, workflow, subject string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := appvalidate.Isolation(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.IsolationInput{
		Workflow:         workflow,
		BuildJob:         "build-and-release",
		SignJob:          "sign-and-publish",
		PrepareJob:       "prepare",
		DistDigestOutput: "dist-digest",
		SigningSecrets:   []string{"COSIGN_SIGNING_KEY", "GPG_SIGNING_KEY"},
		SinglePinSubject: subject,
		FS:               mem.FS(),
	})

	return out.String(), err
}

func runForgejoIsolation(t *testing.T, body string) (string, error) {
	t.Helper()

	mem := testfs.NewMemory(t)
	mem.WriteFile(".forgejo/workflows/release.yml", []byte(body))

	return forgejoIsolation(t, mem, ".forgejo/workflows/release.yml", "itiquette/forgejo-ci")
}

func runForgejoIsolationInFS(t *testing.T, mem *testfs.Memory, workflow string) (string, error) {
	t.Helper()

	return forgejoIsolation(t, mem, workflow, "itiquette/forgejo-ci")
}

const forgejoIsolationCleanWorkflow = `jobs:
  prepare:
    outputs:
      release-tag: ${{ steps.prepare.outputs.release-tag }}
      release-sha: ${{ steps.prepare.outputs.release-sha }}
    steps:
      - name: Check secrets before checkout
        env:
          KEY: ${{ secrets.GPG_SIGNING_KEY }}
      - uses: actions/checkout@1111111111111111111111111111111111111111
        with:
          persist-credentials: false
  build-and-release:
    outputs:
      dist-digest: ${{ steps.handoff.outputs.digest }}
    steps: []
  sign-and-publish:
    uses: itiquette/forgejo-ci/.forgejo/workflows/sign-and-publish-release.yml@1111111111111111111111111111111111111111
    with:
      release-tag: ${{ needs.prepare.outputs.release-tag }}
      release-sha: ${{ needs.prepare.outputs.release-sha }}
      dist-digest: ${{ needs.build-and-release.outputs.dist-digest }}
    secrets:
      COSIGN_SIGNING_KEY: ${{ secrets.COSIGN_SIGNING_KEY }}
      GPG_SIGNING_KEY: ${{ secrets.GPG_SIGNING_KEY }}
`

func TestIsolation_CleanWorkflowPasses(t *testing.T) {
	out, err := runIsolation(t, isolationCleanWorkflow)
	if err != nil {
		t.Fatalf("Isolation: %v", err)
	}

	if !strings.Contains(out, "Static release-isolation checks passed") {
		t.Errorf("missing success line in:\n%s", out)
	}
}

func TestIsolation_BuildJobLeakingSecretFails(t *testing.T) {
	body := `name: release
jobs:
  build:
    runs-on: ubuntu-24.04
    env:
      KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
`

	out, err := runIsolation(t, body)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(out, "COSIGN_PRIVATE_KEY") || !strings.Contains(out, "no access to signing keys") {
		t.Errorf("missing build-job-secret annotation in:\n%s", out)
	}
}

func TestIsolation_CheckoutPersistingCredentialsFails(t *testing.T) {
	tests := map[string]string{
		"missing flag": `name: release
jobs:
  build:
    steps:
      - uses: actions/checkout@v5
`,
		"explicit true": `name: release
jobs:
  build:
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: true
`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := runIsolation(t, body)
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			if !strings.Contains(out, "persist-credentials: false") {
				t.Errorf("missing checkout annotation in:\n%s", out)
			}
		})
	}
}

func TestIsolation_CheckoutExpressionsAndSecrets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		value      string
		wantErrors int
	}{
		{"clean_literal_false", "false", 0},
		{"clean_quoted_false", "'false'", 0},
		{"true_expression", "${{ true }}", 1},
		{"false_expression", "${{ false }}", 1},
		{"secret_expression", "${{ secrets.COSIGN_PRIVATE_KEY }}", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(isolationCleanWorkflow, "persist-credentials: false", "persist-credentials: "+tc.value, 1)

			out, err := runIsolation(t, body)
			if tc.wantErrors == 0 {
				if err != nil || !strings.Contains(out, "Static release-isolation checks passed") {
					t.Fatalf("clean workflow refused: %s %v", out, err)
				}

				return
			}

			if !errors.Is(err, errs.ErrValidation) || strings.Contains(out, "Static release-isolation checks passed") || strings.Count(out, "::error ") != tc.wantErrors {
				t.Fatalf("checkout refusal lost or masked by another rule: %s %v", out, err)
			}

			if !strings.Contains(out, `line=6::job "build": actions/checkout step must set persist-credentials: false`) {
				t.Fatalf("missing checkout annotation: %s", out)
			}

			if tc.wantErrors == 2 && !strings.Contains(out, `line=8::build job "build" references signing secret "COSIGN_PRIVATE_KEY"`) {
				t.Fatalf("checkout check suppressed the independent secret diagnostic: %s", out)
			}
		})
	}
}

// TestIsolation_ReportsEveryViolatingJob covers the scan across jobs.
// Every fixture here has a single job with a single problem, so a check
// that reported the first violation and stopped, or that only inspected
// the first job, would satisfy all of them -- while a workflow whose
// second job persists checkout credentials passed as isolated.
//
// This is a release-isolation invariant: a credential left on disk in any job
// is reachable by anything that job runs, not only by the first one.
func TestIsolation_ReportsEveryViolatingJob(t *testing.T) {
	body := `name: release
jobs:
  prepare:
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
  build:
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
  test:
    steps:
      - uses: actions/checkout@v5
  docs:
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: true
`

	out, err := runIsolation(t, body)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// Two offending jobs, both annotated. Counting the annotations is
	// what distinguishes "found them all" from "found one and stopped".
	if got := strings.Count(out, "persist-credentials: false"); got != 2 {
		t.Errorf("annotated %d checkout violations, want 2:\n%s", got, out)
	}

	// Each on its own line, so the annotations point at the right steps
	// rather than all at the first.
	lines := map[string]bool{}

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "persist-credentials: false") {
			lines[line] = true
		}
	}

	if len(lines) != 2 {
		t.Errorf("both violations annotated at the same location:\n%s", out)
	}
}

func TestIsolation_CheckoutConsumerIsExempt(t *testing.T) {
	body := `name: release
jobs:
  build:
    steps:
      - uses: https://codeberg.org/itiquette/forgejo-ci/actions/checkout-consumer@1111111111111111111111111111111111111111
`

	out, err := runIsolation(t, body)
	if err != nil {
		t.Fatalf("checkout-consumer should be exempt: %v\n%s", err, out)
	}
}

func TestIsolation_ForgejoStrictWorkflowPasses(t *testing.T) {
	out, err := runForgejoIsolation(t, forgejoIsolationCleanWorkflow)
	if err != nil {
		t.Fatalf("Isolation: %v\n%s", err, out)
	}

	if !strings.Contains(out, "Static release-isolation checks passed") {
		t.Errorf("missing success line in:\n%s", out)
	}
}

// TestIsolation_CalledPrepareJobPasses pins the reusable-workflow prepare
// shape (Forgejo v15+): a prepare job that is a `uses:` call gets its
// release-identity outputs from the CALLED workflow, invisible to this
// static pass — the declared-outputs check applies only to step-based
// prepare jobs, while the sign-side needs.prepare checks still apply.
func TestIsolation_CalledPrepareJobPasses(t *testing.T) {
	body := `jobs:
  prepare:
    uses: itiquette/forgejo-ci/.forgejo/workflows/prepare-release.yml@1111111111111111111111111111111111111111
    with:
      forgejo-ci-sha: 1111111111111111111111111111111111111111
    secrets:
      GPG_SIGNING_KEY: ${{ secrets.GPG_SIGNING_KEY }}
      COSIGN_SIGNING_KEY: ${{ secrets.COSIGN_SIGNING_KEY }}
  build-and-release:
    outputs:
      dist-digest: ${{ steps.handoff.outputs.digest }}
    steps: []
  sign-and-publish:
    uses: itiquette/forgejo-ci/.forgejo/workflows/sign-and-publish-release.yml@1111111111111111111111111111111111111111
    with:
      release-tag: ${{ needs.prepare.outputs.release-tag }}
      release-sha: ${{ needs.prepare.outputs.release-sha }}
      dist-digest: ${{ needs.build-and-release.outputs.dist-digest }}
    secrets:
      COSIGN_SIGNING_KEY: ${{ secrets.COSIGN_SIGNING_KEY }}
      GPG_SIGNING_KEY: ${{ secrets.GPG_SIGNING_KEY }}
`

	out, err := runForgejoIsolation(t, body)
	if err != nil {
		t.Fatalf("Isolation: %v\n%s", err, out)
	}

	if !strings.Contains(out, "Static release-isolation checks passed") {
		t.Errorf("missing success line in:\n%s", out)
	}
}

func TestIsolation_ForgejoStrictViolations(t *testing.T) {
	tests := map[string]struct {
		body string
		want string
	}{
		"late_prepare_secret": {strings.Replace(forgejoIsolationCleanWorkflow,
			`      - name: Check secrets before checkout
        env:
          KEY: ${{ secrets.GPG_SIGNING_KEY }}
      - uses: actions/checkout@1111111111111111111111111111111111111111`,
			`      - uses: actions/checkout@1111111111111111111111111111111111111111
      - name: Late secret check
        env:
          KEY: ${{ secrets.GPG_SIGNING_KEY }}`, 1), "at/after checkout"},
		"missing_sign_secret": {strings.Replace(forgejoIsolationCleanWorkflow,
			`      GPG_SIGNING_KEY: ${{ secrets.GPG_SIGNING_KEY }}
`, "", 1), "missing signing secret GPG_SIGNING_KEY"},
		"broken_dist_digest": {strings.Replace(forgejoIsolationCleanWorkflow,
			"dist-digest: ${{ needs.build-and-release.outputs.dist-digest }}",
			"dist-digest: ${{ steps.local.outputs.digest }}", 1), "must pass dist-digest"},
		"cached_toolchain": {strings.Replace(forgejoIsolationCleanWorkflow,
			"steps: []",
			`steps:
      - uses: https://codeberg.org/itiquette/forgejo-ci/actions/setup-toolchain@1111111111111111111111111111111111111111`, 1), "cache: false"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := runForgejoIsolation(t, tc.body)
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation\n%s", err, out)
			}

			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in:\n%s", tc.want, out)
			}
		})
	}
}

func TestIsolation_UnmatchedSinglePinSubjectFails(t *testing.T) {
	// A subject that matches nothing used to pass: the pin set was empty, empty
	// is not "more than one", and the gate reported success. A stale or
	// misspelled subject therefore switched this check off while still printing
	// a tick.
	mem := testfs.NewMemory(t)
	mem.WriteFile(".forgejo/workflows/release.yml", []byte(forgejoIsolationCleanWorkflow))

	out, err := runIsolationForSubject(t, mem, "itiquette/nowhere-ci")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, out)
	}

	if !strings.Contains(out, "matches nothing") {
		t.Errorf("missing unmatched-subject violation in:\n%s", out)
	}
}

// otherSubjectWorkflow is forgejoIsolationCleanWorkflow with the signer
// call pinned to a subject that is NOT this project, so the helpers key
// derived from it ("other-ci") differs from the string the check used to
// hardcode ("forgejo-ci"). That difference is the whole point: a fixture
// under itiquette/forgejo-ci cannot tell the two apart.
const otherSubjectWorkflow = `jobs:
  prepare:
    outputs:
      release-tag: ${{ steps.prepare.outputs.release-tag }}
      release-sha: ${{ steps.prepare.outputs.release-sha }}
    steps:
      - uses: actions/checkout@1111111111111111111111111111111111111111
        with:
          persist-credentials: false
  build-and-release:
    outputs:
      dist-digest: ${{ steps.handoff.outputs.digest }}
    steps: []
  sign-and-publish:
    uses: example/other-ci/.forgejo/workflows/sign.yml@1111111111111111111111111111111111111111
    with:
      release-tag: ${{ needs.prepare.outputs.release-tag }}
      release-sha: ${{ needs.prepare.outputs.release-sha }}
      dist-digest: ${{ needs.build-and-release.outputs.dist-digest }}
    secrets:
      COSIGN_SIGNING_KEY: ${{ secrets.COSIGN_SIGNING_KEY }}
      GPG_SIGNING_KEY: ${{ secrets.GPG_SIGNING_KEY }}
`

// runIsolationForSubject runs the forgejo-shaped gate with a caller-chosen
// single-pin subject.
func runIsolationForSubject(t *testing.T, mem *testfs.Memory, subject string) (string, error) {
	t.Helper()

	return forgejoIsolation(t, mem, ".forgejo/workflows/release.yml", subject)
}

// TestIsolation_HelpersPinKeyFollowsSubject proves the helpers input key
// is derived from the subject's repository rather than hardcoded to this
// project's name.
//
// Both cases use the subject example/other-ci, because that is the only
// way to tell the two implementations apart: for itiquette/forgejo-ci the
// derived key and the old hardcoded string are the same text, so a
// fixture using this project's own name passes either way.
func TestIsolation_HelpersPinKeyFollowsSubject(t *testing.T) {
	t.Run("the subject's own helpers key participates", func(t *testing.T) {
		mem := testfs.NewMemory(t)
		mem.WriteFile(".forgejo/workflows/release.yml", []byte(otherSubjectWorkflow))
		mem.WriteFile(".forgejo/workflows/other.yml", []byte(`jobs:
  probe:
    with:
      other-ci-helpers-sha: 3333333333333333333333333333333333333333
`))

		out, err := runIsolationForSubject(t, mem, "example/other-ci")
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("a helpers pin disagreeing with the uses: pin was not reported: err = %v\n%s", err, out)
		}

		if !strings.Contains(out, "mixed example/other-ci pins") {
			t.Errorf("missing mixed-pin violation from the helpers key in:\n%s", out)
		}
	})

	t.Run("another project's helpers key does not", func(t *testing.T) {
		mem := testfs.NewMemory(t)
		mem.WriteFile(".forgejo/workflows/release.yml", []byte(otherSubjectWorkflow))
		mem.WriteFile(".forgejo/workflows/other.yml", []byte(`jobs:
  probe:
    with:
      forgejo-ci-helpers-sha: 3333333333333333333333333333333333333333
`))

		// The workflow directory holds one example/other-ci pin and one
		// sha belonging to a different project. Only the first is this
		// subject's, so the pins are consistent and the gate passes.
		out, err := runIsolationForSubject(t, mem, "example/other-ci")
		if err != nil {
			t.Fatalf("a foreign project's helpers sha was counted as this subject's pin: %v\n%s", err, out)
		}
	})
}

func TestIsolation_MixedPinsFail(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".forgejo/workflows/release.yml", []byte(forgejoIsolationCleanWorkflow))
	mem.WriteFile(".forgejo/workflows/other.yml", []byte(`jobs:
  probe:
    steps:
      - uses: https://codeberg.org/itiquette/forgejo-ci/actions/setup-buildah@2222222222222222222222222222222222222222
`))

	out, err := runForgejoIsolationInFS(t, mem, ".forgejo/workflows/release.yml")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, out)
	}

	if !strings.Contains(out, "mixed itiquette/forgejo-ci pins") {
		t.Errorf("missing mixed-pin violation in:\n%s", out)
	}
}

func TestIsolation_CommentedPinDoesNotCreateAConflict(t *testing.T) {
	mem := testfs.NewMemory(t)
	mem.WriteFile(".forgejo/workflows/release.yml", []byte(forgejoIsolationCleanWorkflow))
	mem.WriteFile(".forgejo/workflows/other.yml", []byte(`# uses: https://codeberg.org/itiquette/forgejo-ci/actions/setup-buildah@2222222222222222222222222222222222222222
jobs:
  probe:
    steps:
      - run: echo no pin here
`))

	out, err := runForgejoIsolationInFS(t, mem, ".forgejo/workflows/release.yml")
	if err != nil {
		t.Fatalf("commented pin created a conflict: %v\n%s", err, out)
	}
}

func TestIsolation_SiblingSignerContract(t *testing.T) {
	signerWorkflow := `# workflow-call-secrets-contract: COSIGN_SIGNING_KEY GPG_SIGNING_KEY
jobs:
  sign-and-publish:
    steps:
      - name: Verify dist integrity
        env:
          EXPECTED_DIGEST: ${{ inputs.dist-digest }}
        run: reusable-ci release validate-dist --dist-dir dist --expected-digest "${EXPECTED_DIGEST}"
`

	mem := testfs.NewMemory(t)
	mem.WriteFile(".scratch/consumer/.forgejo/workflows/release.yml", []byte(forgejoIsolationCleanWorkflow))
	mem.WriteFile(".scratch/forgejo-ci/.forgejo/workflows/sign-and-publish-release.yml", []byte(signerWorkflow))

	out, err := runForgejoIsolationInFS(t, mem, ".scratch/consumer/.forgejo/workflows/release.yml")
	if err != nil {
		t.Fatalf("clean sibling contract rejected: %v\n%s", err, out)
	}

	mem.WriteFile(".scratch/forgejo-ci/.forgejo/workflows/sign-and-publish-release.yml", []byte(strings.Replace(signerWorkflow, " GPG_SIGNING_KEY", "", 1)))

	out, err = runForgejoIsolationInFS(t, mem, ".scratch/consumer/.forgejo/workflows/release.yml")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, out)
	}

	if !strings.Contains(out, "does not list it") {
		t.Errorf("missing sibling contract violation in:\n%s", out)
	}
}

func TestIsolation_SecretScalarAnnotations(t *testing.T) {
	body := "jobs:\n  build:\n    steps:\n      - run: >-\n          ${{ secrets.COSIGN_PRIVATE_KEY }}\n          ${{ secrets.RELEASE_GPG_PRIVATE_KEY }}\n      - env:\n          KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}\n"

	out, err := runIsolation(t, body)
	if !errors.Is(err, errs.ErrValidation) || strings.Contains(out, "Static release-isolation checks passed") {
		t.Fatalf("unsafe expressions did not refuse cleanly: %s %v", out, err)
	}

	for _, annotation := range []string{
		`::error file=.github/workflows/release.yml,line=4::build job "build" references signing secret "COSIGN_PRIVATE_KEY"`,
		`::error file=.github/workflows/release.yml,line=4::build job "build" references signing secret "RELEASE_GPG_PRIVATE_KEY"`,
		`::error file=.github/workflows/release.yml,line=8::build job "build" references signing secret "COSIGN_PRIVATE_KEY"`,
	} {
		if strings.Count(out, annotation) != 1 {
			t.Fatalf("missing/duplicate scalar annotation %q: %s", annotation, out)
		}
	}
}

func TestIsolation_SiblingStaticScope(t *testing.T) {
	const (
		header  = "# workflow-call-secrets-contract: COSIGN_SIGNING_KEY GPG_SIGNING_KEY\n"
		channel = "on:\n  workflow_call:\n    inputs:\n      dist-digest:\n        type: string\n"
	)
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"absent_checkout", "", "Sibling signer static contract not checked"},
		{"name_only", header + "name: validate-dist inputs.dist-digest\njobs: {}\n", "lexical only, not proof of input consumption"},
		{"comment_only", header + "# validate-dist inputs.dist-digest\njobs: {}\n", "has no literal dist-digest input reference"},
		{"description_only", header + "description: validate-dist inputs.dist-digest\njobs: {}\n", "lexical only, not proof of input consumption"},
		{"wrong_input", header + strings.Replace(channel, "dist-digest:", "other-digest:", 1) + "name: validate-dist inputs.dist-digest\njobs: {}\n", "lexical only, not proof of input consumption"},
		{"declaration_without_reference", header + channel + "jobs: {}\n", "has no literal dist-digest input reference"},
		{"reference_without_verifier", header + channel + "name: inputs.dist-digest\njobs: {}\n", "lexical only, not proof of input consumption"},
		{"late_verifier", header + channel + "jobs:\n  sign:\n    steps:\n      - run: sign-placeholder\n      - run: reusable-ci release validate-dist --expected-digest '${{ inputs.dist-digest }}'\n", "lexical only, not proof of input consumption"},
		{"wrong_runtime_input", header + channel + "name: inputs.dist-digest\njobs:\n  sign:\n    steps:\n      - run: reusable-ci release validate-dist --expected-digest wrong\n", "lexical only, not proof of input consumption"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := testfs.NewMemory(t)
			mem.WriteFile(".scratch/consumer/.forgejo/workflows/release.yml", []byte(forgejoIsolationCleanWorkflow))

			if tc.body != "" {
				mem.WriteFile(".scratch/forgejo-ci/.forgejo/workflows/sign-and-publish-release.yml", []byte(tc.body))
			}

			out, err := runForgejoIsolationInFS(t, mem, ".scratch/consumer/.forgejo/workflows/release.yml")

			wantFailure := strings.Contains(tc.want, "has no literal")
			if (err != nil) != wantFailure || (wantFailure && !errors.Is(err, errs.ErrValidation)) || !strings.Contains(out, tc.want) {
				t.Fatalf("out=%s err=%v want=%s", out, err, tc.want)
			}

			if !strings.Contains(out, "Digest verification before signing is not checked; the owner of the pinned signing workflow must enforce the runtime verifier and ordering.") || strings.Contains(out, "release-ci") {
				t.Fatalf("missing runtime ownership/scope notice: %s", out)
			}

			if wantFailure && strings.Contains(out, "Static release-isolation checks passed") {
				t.Fatalf("failed static contract reported success: %s", out)
			}
		})
	}
}
