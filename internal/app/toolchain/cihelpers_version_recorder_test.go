// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const changelogRecorderRoot = "REUSABLE_CI_OWNED_CHANGELOG_RECORDER"

type changelogVersionInvocation struct {
	Args       []string `json:"args"`
	Env        []string `json:"env"`
	PathBytes  string   `json:"path_bytes"`
	LinkTarget string   `json:"link_target"`
}

func TestMain(m *testing.M) {
	root, bin := os.Getenv(changelogRecorderRoot), filepath.Base(os.Args[0])
	if root != "" || bin == gitCliffBin || bin == gitChglogBin {
		// Dispatch before flag parsing, even for wrong argv. A marked child must
		// never recurse into the test suite; a lost marker fails closed as well.
		os.Exit(recordChangelogVersion(root))
	}

	os.Exit(m.Run())
}

func recordChangelogVersion(root string) int {
	timer := time.AfterFunc(5*time.Second, func() { os.Exit(124) })
	defer timer.Stop()

	bin := filepath.Base(os.Args[0])
	if !filepath.IsAbs(root) || (bin != "git-cliff" && bin != "git-chglog") || filepath.Dir(os.Args[0]) != filepath.Join(root, "renderer bin") {
		return 125
	}
	//nolint:gosec // marker and argv name bind this child to the parent's owned fixture root.
	pathBytes, err := os.ReadFile(filepath.Join(root, runnerPathName))
	if err != nil {
		return 126
	}

	linkTarget, err := os.Readlink(os.Args[0])
	if err != nil {
		return 126
	}
	//nolint:gosec // exclusive private record inside the parent's owned fixture root, never an ambient output.
	file, err := os.OpenFile(filepath.Join(root, "version.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 126
	}

	encodeErr := json.NewEncoder(file).Encode(changelogVersionInvocation{
		Args: os.Args, Env: os.Environ(), PathBytes: string(pathBytes), LinkTarget: linkTarget,
	})

	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		return 126
	}

	if os.Getenv("REUSABLE_CI_OWNED_CHANGELOG_FAIL") == "yes" {
		fmt.Fprint(os.Stderr, "owned version failure output")

		return 17
	}

	_, _ = fmt.Fprint(os.Stdout, "owned version stdout\n")
	fmt.Fprint(os.Stderr, "owned version stderr")

	return 0
}
