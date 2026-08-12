// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestDebugWorkspace_PrintsAllSections(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("README.md", []byte("hi"))
	fsys.WriteFile(".github-shared/scripts/validate/validate-x.sh", []byte("#!/bin/sh"))

	var out bytes.Buffer

	err := appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{
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
	if err := appplatform.DebugWorkspace(&out, appplatform.DebugWorkspaceInput{Root: testfs.NewReal(t).Root}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), ".github-shared not found") {
		t.Errorf("expected missing-shared notice:\n%s", out.String())
	}
}
