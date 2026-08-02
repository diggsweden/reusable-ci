// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type fakeMiseRunner struct {
	responses map[string]string
	calls     []fakeMiseCall
}

type fakeMiseCall struct {
	env  []string
	args []string
}

func (f *fakeMiseRunner) Run(_ context.Context, env []string, args ...string) (string, error) {
	f.calls = append(f.calls, fakeMiseCall{env: append([]string{}, env...), args: append([]string{}, args...)})

	key := strings.Join(args, "\x00")
	if value, ok := f.responses[key]; ok {
		return value, nil
	}

	return "", nil
}

func TestExposeMiseTools_AppendsBinPathsAndSymlinksExecutables(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binHome := filepath.Join(root, "home-bin")
	pathFile := filepath.Join(root, "path-file")
	toolBin := filepath.Join(root, "mise-tools", "bin")
	writeExecutable(t, filepath.Join(toolBin, "goreleaser"))
	writeToolchainFile(t, toolBin, "README", "not executable\n")

	runner := &fakeMiseRunner{responses: map[string]string{
		"ls\x00--installed": "goreleaser 2.12.0",
		"bin-paths":         toolBin + "\n" + filepath.Join(root, "missing"),
	}}

	var out bytes.Buffer

	err := toolchain.ExposeMiseTools(context.Background(), runner, &out, toolchain.ExposeMiseToolsInput{
		Root:     root,
		BinHome:  binHome,
		PathFile: pathFile,
		Locked:   "false",
	})
	if err != nil {
		t.Fatalf("ExposeMiseTools: %v", err)
	}

	body, err := os.ReadFile(pathFile) //nolint:gosec // test-owned path.
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != toolBin+"\n" {
		t.Fatalf("path file = %q", string(body))
	}

	link, err := os.Readlink(filepath.Join(binHome, "goreleaser"))
	if err != nil {
		t.Fatal(err)
	}

	if link != filepath.Join(toolBin, "goreleaser") {
		t.Fatalf("symlink target = %q", link)
	}

	if _, err := os.Lstat(filepath.Join(binHome, "README")); !os.IsNotExist(err) {
		t.Fatalf("non-executable should not be linked, err=%v", err)
	}

	if !strings.Contains(out.String(), "::group::setup-toolchain: installed tools") || !strings.Contains(out.String(), "goreleaser 2.12.0") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestExposeMiseTools_ExposesLockedRustupCargoBins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\n\"aqua:rust-lang/rustup\" = \"1.28.2\"\n")
	writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel = \"1.90.0\"\n")
	binHome := filepath.Join(root, "home-bin")
	pathFile := filepath.Join(root, "path-file")
	cargoBin := filepath.Join(root, "rustup", "bin")
	writeExecutable(t, filepath.Join(cargoBin, "cargo"))

	runner := &fakeMiseRunner{responses: map[string]string{
		"bin-paths": "",
		"exec\x00--no-deps\x00--locked\x00aqua:rust-lang/rustup\x00--\x00rustup\x00which\x00cargo": filepath.Join(cargoBin, "cargo"),
	}}

	err := toolchain.ExposeMiseTools(context.Background(), runner, nil, toolchain.ExposeMiseToolsInput{
		Root:     root,
		BinHome:  binHome,
		PathFile: pathFile,
		Locked:   "true",
	})
	if err != nil {
		t.Fatalf("ExposeMiseTools: %v", err)
	}

	wantArgs := []string{"exec", "--no-deps", "--locked", "aqua:rust-lang/rustup", "--", "rustup", "which", "cargo"}

	gotCall := runner.calls[len(runner.calls)-1]
	if !reflect.DeepEqual(gotCall.args, wantArgs) {
		t.Fatalf("cargo args = %#v", gotCall.args)
	}

	if !envContains(gotCall.env, "MISE_LOCKED_VERIFY_PROVENANCE=0") || !envContains(gotCall.env, "MISE_PARANOID=0") {
		t.Fatalf("locked env missing: %#v", gotCall.env)
	}

	body, err := os.ReadFile(pathFile) //nolint:gosec // test-owned path.
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != cargoBin+"\n" {
		t.Fatalf("path file = %q", string(body))
	}

	if _, err := os.Readlink(filepath.Join(binHome, "cargo")); err != nil {
		t.Fatal(err)
	}
}

func TestExposeMiseTools_RequiresPathFileAndLockedMode(t *testing.T) {
	t.Parallel()

	err := toolchain.ExposeMiseTools(context.Background(), &fakeMiseRunner{}, nil, toolchain.ExposeMiseToolsInput{Root: t.TempDir(), Locked: "false"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("missing path file err = %v, want ErrUsage", err)
	}

	err = toolchain.ExposeMiseTools(context.Background(), &fakeMiseRunner{}, nil, toolchain.ExposeMiseToolsInput{Root: t.TempDir(), PathFile: "path", Locked: "yes"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("bad locked err = %v, want ErrUsage", err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}
}

func envContains(env []string, want string) bool {
	for _, got := range env {
		if got == want {
			return true
		}
	}

	return false
}
