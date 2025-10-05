// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestReleaseImageVerify_LocalInputsAreValidatedBeforeAnyRegistryWork runs the
// verb's local validation through its real flag set. The trusted key path must
// be relative to the checkout, as the base-image verbs already required; a
// pinned digest must be hex and match a regular, non-linked key file; an
// unpinned key is not read; the expected workflow path must be relative with no
// parent step in any position, where the old substring check let "..",
// "dir/.." and a tab through.
func TestReleaseImageVerify_LocalInputsAreValidatedBeforeAnyRegistryWork(t *testing.T) {
	const workflow = ".github/workflows/release.yml"

	key := []byte("-----BEGIN PUBLIC KEY-----\nfixture\n-----END PUBLIC KEY-----\n")
	sum := sha256.Sum256(key)
	pin := hex.EncodeToString(sum[:])

	for _, tc := range []struct {
		name, keyPath, keySHA, workflow string
		want                            error
	}{
		{"pinned key matches", "keys/cosign.pub", pin, workflow, nil},
		{"unpinned key is not read", "keys/absent.pub", "", workflow, nil},
		{"key path blank", " ", pin, workflow, errs.ErrUsage},
		{"key path absolute", "/etc/cosign.pub", "", workflow, errs.ErrUsage},
		{"key path climbs", "keys/../../cosign.pub", "", workflow, errs.ErrUsage},
		{"pin is not hex", "keys/cosign.pub", "not-a-digest", workflow, errs.ErrValidation},
		{"pinned key missing", "keys/absent.pub", pin, workflow, errs.ErrMissingInput},
		{"pinned key is a directory", "keys", pin, workflow, errs.ErrMissingInput},
		{"pinned key is a link", "keys/link.pub", pin, workflow, errs.ErrMissingInput},
		{"pinned key differs", "keys/other.pub", pin, workflow, errs.ErrValidation},
		{"workflow is the parent", "keys/cosign.pub", pin, "..", errs.ErrUsage},
		{"workflow ends in a parent step", "keys/cosign.pub", pin, ".github/workflows/..", errs.ErrUsage},
		{"workflow climbs mid-path", "keys/cosign.pub", pin, ".github/../../workflow.yml", errs.ErrUsage},
		{"workflow absolute", "keys/cosign.pub", pin, "/workflow.yml", errs.ErrUsage},
		{"workflow with a tab", "keys/cosign.pub", pin, ".github/workflows/re\tlease.yml", errs.ErrUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testenv.New(t)

			fsys := testfs.NewReal(t)
			fsys.WriteFile("keys/cosign.pub", key)
			fsys.WriteFile("keys/other.pub", []byte("another key\n"))
			fsys.Chdir()

			if err := os.Symlink("cosign.pub", "keys/link.pub"); err != nil {
				t.Fatal(err)
			}

			cmd := releaseImagesVerifyCmd()

			var validateErr error

			cmd.Action = func(_ context.Context, parsed *cli.Command) error {
				validateErr = validateReleaseImageCLIInput(parsed)

				return nil
			}

			args := []string{cmd.Name, "--ref", "registry.example/app@sha256:" + strings.Repeat("a", 64), "--cosign-public-key-path", tc.keyPath, "--cosign-public-key-sha256", tc.keySHA,
				"--expected-tag", "v1.2.3", "--expected-commit", strings.Repeat("c", 40), "--expected-source", "https://forge.example/org/app", "--expected-workflow", tc.workflow}
			if err := cmd.Run(t.Context(), args); err != nil {
				t.Fatal(err)
			}

			if tc.want == nil {
				if validateErr != nil {
					t.Fatalf("err = %v, want accepted", validateErr)
				}

				return
			}

			if !errors.Is(validateErr, tc.want) {
				t.Fatalf("err = %v, want %v", validateErr, tc.want)
			}
		})
	}
}
