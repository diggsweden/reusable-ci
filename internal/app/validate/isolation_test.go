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
