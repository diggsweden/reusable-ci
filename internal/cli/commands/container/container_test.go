// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	containercmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/glabenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestRefResolveCmd_WritesOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("CONTAINER_REGISTRY", "ghcr.io")
	env.Setenv("REPOSITORY", "owner/repo")
	env.Setenv("REPOSITORY_OWNER", "owner")

	cmd := containercmd.New()
	if err := cmd.Run(context.Background(), []string{"container", "ref", "resolve"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("name"); got != "ghcr.io/owner/repo" {
		t.Errorf("name = %q", got)
	}
}

func TestValidateContainerfileCmd_WritesOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("Containerfile", []byte("FROM alpine\n"))

	cmd := containercmd.New()
	if err := cmd.Run(context.Background(), []string{"container", "validate", "containerfile", "--path", path}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("containerfile"); got != path {
		t.Errorf("containerfile = %q", got)
	}
}

func TestCommands_RequireFlagsWhenMissing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "validate_containerfile", args: []string{"container", "validate", "containerfile"}, want: `Required flag "path" not set`},
		{name: "validate_artifacts_missing_artifact_dir", args: []string{"container", "validate", "artifacts", "--project-type", "maven"}, want: `Required flag "artifact-dir" not set`},
		{name: "suffix_extracted_binaries_missing_arch", args: []string{"container", "suffix-extracted-binaries", "--binaries-dir", "./extracted"}, want: `Required flag "arch" not set`},
		{name: "ledger_rollback_requires_journal", args: []string{"container", "ledger", "rollback"}, want: `--journal is required because rollback without pre-promotion state cannot prove tag ownership`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := containercmd.New()

			err := cmd.Run(context.Background(), testCase.args)
			if err == nil {
				t.Fatal("expected a refusal")
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want substring %q", err, testCase.want)
			}

			// Each of these must exit 2, not the unclassified 70 that reads
			// "file a bug". urfave's own "Required flag" errors carry no
			// sentinel, so the classification happens where main.go does it --
			// running the error through the same function is what pins the
			// exit code, rather than a bare "some error came back".
			if got := errs.ExitCodeFromError(cli.ClassifyError(err)); got != errs.ExitCodeUsage {
				t.Errorf("exit code = %d, want usage (%d)", got, errs.ExitCodeUsage)
			}
		})
	}
}

// A successful login must not be turned into a failure by the step-output
// convenience that follows it.
//
// GitHub and Forgejo runners always provide an output file, so the unconditional
// write there is invisible. GitLab provides none unless the pipeline nominates
// one in $CI_OUTPUT. Returning that sink's error means the credential is
// written, the login has genuinely succeeded, and the command still exits
// non-zero. Found by PAR-REG-5 against a real runner; guarded here so it cannot
// come back without the live tier being run.
func TestLoginCmd_OnGitLabWithoutAnOutputFile_StillSucceeds(t *testing.T) {
	env := glabenv.Setup(t)

	// The pipeline nominated no dotenv report, which is the ordinary case: a job
	// that only logs in has no reason to declare one.
	env.Setenv("CI_OUTPUT", "")
	env.Setenv("REUSABLE_CI_RUNNER", "gitlab")
	env.Setenv("REGISTRY_TOKEN", "s3cret")

	fsys := testfs.NewReal(t)
	authFile := filepath.Join(filepath.Dir(fsys.WriteFile("anchor", nil)), "auth.json")

	cmd := containercmd.New()

	err := cmd.Run(context.Background(), []string{
		"container", "login",
		"--registry", "registry.example.com",
		"--registry-username", "ci",
		"--auth-file", authFile,
	})
	if err != nil {
		t.Fatalf("login failed although the credential was the only thing asked for: %v", err)
	}

	// The credential still has to have been written: degrading the output must
	// not degrade the login itself.
	data, readErr := os.ReadFile(authFile)
	if readErr != nil {
		t.Fatalf("read auth file: %v", readErr)
	}

	want := base64.StdEncoding.EncodeToString([]byte("ci:s3cret"))
	if !strings.Contains(string(data), want) {
		t.Errorf("auth file does not carry the credential\ngot: %s", data)
	}
}

// TestLedgerAddCmd_UnreadableLedgerIsNotOverwritten pins that `ledger add`
// refuses a ledger it cannot read instead of treating it as empty: the
// read-modify-write under the lock otherwise rewrote the file with the new
// entry alone, silently dropping every image recorded before it.
func TestLedgerAddCmd_UnreadableLedgerIsNotOverwritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0200 file regardless of its mode")
	}

	fsys := testfs.NewReal(t)
	path := fsys.Path("release-images.json")

	add := func(hex, suffix string) error {
		return containercmd.New().Run(context.Background(), []string{
			"container", "ledger", "add",
			"--ledger", path, "--tag", "v1.2.3", "--role", "role-" + suffix,
			"--ref", "registry.example/owner/app@sha256:" + hex, "--digest", "sha256:" + hex,
			"--final-tag", "registry.example/owner/app:v1.2.3-" + suffix,
		})
	}

	if err := add(strings.Repeat("a", 64), "a"); err != nil {
		t.Fatalf("first add: %v", err)
	}

	before := fsys.ReadFile("release-images.json")

	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}

	err := add(strings.Repeat("b", 64), "b")

	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		t.Fatal(chmodErr)
	}

	if err == nil {
		t.Fatal("ledger add succeeded against a ledger it could not read")
	}

	if got := fsys.ReadFile("release-images.json"); !bytes.Equal(got, before) {
		t.Errorf("ledger was rewritten although the read failed\nbefore: %s\nafter:  %s", before, got)
	}
}
