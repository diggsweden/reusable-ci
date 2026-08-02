// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	versioncmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/version"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestVersionMutatingVerbsHaveDryRun is the guardrail: every version verb
// that pushes to the remote must expose --dry-run so operators can preview
// the git mutations before performing them (matching the ledger convention).
func TestVersionMutatingVerbsHaveDryRun(t *testing.T) {
	t.Parallel()

	required := map[string]bool{"tag-release": true, "commit-push": true, "commit-changelog-release": true}

	for _, sub := range versioncmd.New().Commands {
		if !required[sub.Name] {
			continue
		}

		found := false

		for _, flag := range sub.Flags {
			if slices.Contains(flag.Names(), "dry-run") {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("version %q is a mutating verb and must expose --dry-run", sub.Name)
		}

		delete(required, sub.Name)
	}

	for name := range required {
		t.Errorf("expected version subcommand %q not found", name)
	}
}

func TestBumpCmd_RequiresProjectTypeAndVersion(t *testing.T) {
	cmd := versioncmd.New()

	err := cmd.Run(context.Background(), []string{"version", "bump"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err == nil || !strings.Contains(err.Error(), `Required flags "project-type, version" not set`) {
		t.Errorf("err = %v", err)
	}
}

// TestBumpCmd_XcodeGradleVersionFileIsIgnored proves that --gradle-version-file
// is ignored for the xcode-ios path (xcode-ios always rewrites versions.xcconfig
// unless --xcode-version-file points elsewhere).
func TestBumpCmd_XcodeGradleVersionFileIsIgnored(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("custom.xcconfig", []byte("MARKETING_VERSION = 0.5.0\n"))

	env := testenv.New(t)
	env.Setenv("PROJECT_TYPE", "xcode-ios")
	env.Setenv("VERSION", "1.0.0")
	env.Setenv("WORKING_DIRECTORY", dir)
	// --gradle-version-file is xcode-irrelevant; it must not steer the run.
	env.Setenv("GRADLE_VERSION_FILE", "custom.xcconfig")

	cmd := versioncmd.New()
	if err := cmd.Run(context.Background(), []string{"version", "bump"}); err != nil {
		t.Fatal(err)
	}

	body := fsys.ReadFile("versions.xcconfig")
	if string(body) != "MARKETING_VERSION = 1.0.0\n" {
		t.Errorf("versions.xcconfig = %q", body)
	}

	customBody := fsys.ReadFile("custom.xcconfig")
	if !strings.Contains(string(customBody), "0.5.0") {
		t.Errorf("custom.xcconfig should be unchanged, got %q", customBody)
	}
}

func TestBumpCmd_GradleVersionFileIsUsed(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("custom.properties", []byte("version=0.1.0\n"))

	env := testenv.New(t)
	env.Setenv("PROJECT_TYPE", "gradle")
	env.Setenv("VERSION", "2.0.0")
	env.Setenv("WORKING_DIRECTORY", dir)
	env.Setenv("GRADLE_VERSION_FILE", "custom.properties")

	cmd := versioncmd.New()
	if err := cmd.Run(context.Background(), []string{"version", "bump"}); err != nil {
		t.Fatal(err)
	}

	body := fsys.ReadFile("custom.properties")
	if !strings.Contains(string(body), "version=2.0.0") {
		t.Errorf("custom.properties = %q", body)
	}
}
