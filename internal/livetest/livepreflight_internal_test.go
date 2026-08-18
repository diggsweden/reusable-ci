// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCAFixture(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestReadCAFile_AcceptsOwnerControlledPublicReadableCertificate(t *testing.T) {
	t.Parallel()

	body := independentCAPEM(t)
	path := filepath.Join(t.TempDir(), "ca.pem")
	writeCAFixture(t, path, body, 0o644)

	got, err := readCAFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, body) {
		t.Fatal("frozen CA differs from the bytes read through the bound descriptor")
	}
}

func TestReadCAFile_RejectsWritableOrSymlinkedSource(t *testing.T) {
	t.Parallel()

	body := independentCAPEM(t)
	t.Run("group or world writable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		writeCAFixture(t, path, body, 0o666)

		if _, err := readCAFile(path, nil); err == nil || !strings.Contains(err.Error(), "writable by group or other") {
			t.Fatalf("writable CA result = %v", err)
		}
	})
	t.Run("symlink parent", func(t *testing.T) {
		root := t.TempDir()

		realParent := filepath.Join(root, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}

		writeCAFixture(t, filepath.Join(realParent, "ca.pem"), body, 0o644)

		linkParent := filepath.Join(root, "linked")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Fatal(err)
		}

		if _, err := readCAFile(filepath.Join(linkParent, "ca.pem"), nil); err == nil || !strings.Contains(err.Error(), "symbolic links") {
			t.Fatalf("symlink-parent CA result = %v", err)
		}
	})
}

func TestReadCAFile_RejectsDeterministicReplacementAndMutation(t *testing.T) {
	t.Parallel()

	t.Run("replacement after inspection", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		writeCAFixture(t, path, independentCAPEM(t), 0o600)

		hook := func(phase caReadPhase) {
			if phase != caSourceInspected {
				return
			}

			if err := os.Rename(path, path+".old"); err != nil {
				t.Fatal(err)
			}

			writeCAFixture(t, path, independentCAPEM(t), 0o600)
		}

		if _, err := readCAFile(path, hook); err == nil || !strings.Contains(err.Error(), "changed while it was opened") {
			t.Fatalf("replaced CA result = %v", err)
		}
	})
	t.Run("in-place mutation after read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		body := independentCAPEM(t)
		writeCAFixture(t, path, body, 0o600)

		hook := func(phase caReadPhase) {
			if phase == caSourceRead {
				writeCAFixture(t, path, append(body, '\n'), 0o600)
			}
		}

		if _, err := readCAFile(path, hook); err == nil || !strings.Contains(err.Error(), "changed while it was read") {
			t.Fatalf("mutated CA result = %v", err)
		}
	})
}

func TestVerifyCleanupBindings_UsesCapturedIdentityAndDigest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	command := filepath.Join(dir, "cleanup")
	contractFile := filepath.Join(dir, "recovery.env")

	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // Executable fixture must be owner-executable.
		t.Fatal(err)
	}

	if err := os.WriteFile(contractFile, []byte("recovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	commandBinding, contractBinding, err := bindCleanupFiles(command, contractFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyCleanupBindings(command, commandBinding.Facts, contractFile, contractBinding.Facts); err != nil {
		t.Fatalf("unchanged cleanup bindings rejected: %v", err)
	}

	if err := os.WriteFile(contractFile, []byte("mutated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := VerifyCleanupBindings(command, commandBinding.Facts, contractFile, contractBinding.Facts); err == nil ||
		!strings.Contains(err.Error(), "cleanup contract_file changed") {
		t.Fatalf("mutated cleanup binding result = %v", err)
	}
}
