// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const (
	presetRecorderName = "p018-preset-recorder"
	presetRecordName   = "invocation.json"
)

type presetInvocation struct {
	Args  []string `json:"args"`
	Stdin []byte   `json:"stdin"`
	Env   []string `json:"env"`
}

func TestMain(m *testing.M) {
	// Dispatch before testing parses flags: wrong argv must still be recorded,
	// and NewIsolated must not need an environment marker to reach the child.
	if filepath.Base(os.Args[0]) == presetRecorderName {
		os.Exit(recordPresetInvocation())
	}

	os.Exit(m.Run())
}

func recordPresetInvocation() int {
	const stdinLimit = 4096

	stdin, err := io.ReadAll(io.LimitReader(os.Stdin, stdinLimit+1))
	if err != nil || len(stdin) > stdinLimit {
		fmt.Fprintf(os.Stderr, "recorder stdin: bytes=%d limit=%d error=%v\n", len(stdin), stdinLimit, err)

		return 1
	}

	// Use the owned symlink's directory, not the resolved test executable's.
	// Exclusive creation makes a second invocation fail rather than overwrite.
	//nolint:gosec // argv[0] is the absolute symlink path in the parent's owned t.TempDir.
	file, err := os.OpenFile(filepath.Join(filepath.Dir(os.Args[0]), presetRecordName),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "recorder create: %v\n", err)

		return 1
	}

	encodeErr := json.NewEncoder(file).Encode(presetInvocation{
		Args: os.Args[1:], Stdin: stdin, Env: os.Environ(),
	})

	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		fmt.Fprintf(os.Stderr, "recorder write: %v; close: %v\n", encodeErr, closeErr)

		return 1
	}

	return 0
}
