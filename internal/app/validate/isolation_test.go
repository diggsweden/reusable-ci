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

func runForgejoIsolation(t *testing.T, body string) (string, error) {
	t.Helper()

	mem := testfs.NewMemory(t)
	mem.WriteFile(".forgejo/workflows/release.yml", []byte(body))

	var out bytes.Buffer

	err := appvalidate.Isolation(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.IsolationInput{
		Workflow:         ".forgejo/workflows/release.yml",
		BuildJob:         "build-and-release",
		SignJob:          "sign-and-publish",
		PrepareJob:       "prepare",
		DistDigestOutput: "dist-digest",
		SigningSecrets:   []string{"COSIGN_SIGNING_KEY", "GPG_SIGNING_KEY"},
		SinglePinSubject: "itiquette/forgejo-ci",
		FS:               mem.FS(),
	})

	return out.String(), err
}

func runForgejoIsolationInFS(t *testing.T, mem *testfs.Memory, workflow string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := appvalidate.Isolation(&out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.IsolationInput{
		Workflow:         workflow,
		BuildJob:         "build-and-release",
		SignJob:          "sign-and-publish",
		PrepareJob:       "prepare",
		DistDigestOutput: "dist-digest",
		SigningSecrets:   []string{"COSIGN_SIGNING_KEY", "GPG_SIGNING_KEY"},
		SinglePinSubject: "itiquette/forgejo-ci",
		FS:               mem.FS(),
	})

	return out.String(), err
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

	if !strings.Contains(out, "SLSA Build L3 isolation invariants hold") {
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

	if !strings.Contains(out, "SLSA Build L3 isolation invariants hold") {
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
