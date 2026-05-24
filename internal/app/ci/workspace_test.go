// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package ci_test

import (
	"bytes"
	"strings"
	"testing"

	appci "github.com/diggsweden/reusable-ci/internal/app/ci"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestDebugWorkspace_PrintsAllSections(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("README.md", []byte("hi"))
	fsys.WriteFile(".github-shared/scripts/validate/validate-x.sh", []byte("#!/bin/sh"))

	var out bytes.Buffer

	err := appci.DebugWorkspace(&out, appci.DebugWorkspaceInput{
		Root:             fsys.Root,
		ActionRepository: "diggsweden/reusable-ci",
		ActionRef:        "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := out.String()
	for _, want := range []string{
		"=== Workspace structure ===",
		"=== .github-shared structure ===",
		"=== Looking for scripts ===",
		"validate-x.sh",
		"=== GitHub context ===",
		"action_repository: diggsweden/reusable-ci",
		"action_ref: main",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestDebugWorkspace_AnnouncesMissingShared(t *testing.T) {
	var out bytes.Buffer
	if err := appci.DebugWorkspace(&out, appci.DebugWorkspaceInput{Root: testfs.NewReal(t).Root}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), ".github-shared not found") {
		t.Errorf("expected missing-shared notice:\n%s", out.String())
	}
}
