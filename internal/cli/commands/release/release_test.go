// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"os"
	"strings"
	"testing"

	releasecmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestResolveArtifactNameCmd_RequiresProjectTypeFlag(t *testing.T) {
	cmd := releasecmd.New()

	err := cmd.Run(context.Background(), []string{"release", "resolve", "artifact-name"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err == nil || !strings.Contains(err.Error(), `Required flag "project-type" not set`) {
		t.Errorf("err = %v", err)
	}
}

func TestResolveMetadataCmd_WritesOutputsFromEnv(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("VERSION", "v1.2.3")
	env.Setenv("REPOSITORY", "diggsweden/reusable-ci")
	env.Setenv("ARTIFACT_NAME", "artifact-name")

	cmd := releasecmd.New()
	if err := cmd.Run(context.Background(), []string{"release", "resolve", "metadata"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("version"); got != "v1.2.3" {
		t.Errorf("version = %q", got)
	}

	if got := env.Output("version-no-v"); got != "1.2.3" {
		t.Errorf("version-no-v = %q", got)
	}

	if got := env.Output("project-name"); got != "artifact-name" {
		t.Errorf("project-name = %q", got)
	}
}

func TestNotesCmd_UsesDefaultsAndWritesFallbackContentFromEnv(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	env := ghaenv.Setup(t)
	env.Setenv("RELEASE_VERSION", "v2.0.0")
	env.Setenv("RELEASE_COMMIT", "abc1234")

	cmd := releasecmd.New()
	if err := cmd.Run(context.Background(), []string{"release", "notes"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("release-notes.md")); err != nil {
		t.Errorf("default target not created: %v", err)
	}

	body := fsys.ReadFile("release-notes.md")
	for _, want := range []string{"# Release v2.0.0", "Release created from commit abc1234"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("release-notes.md missing %q:\n%s", want, body)
		}
	}
}

func TestVerifyChangelogCmd_RequiresFlag(t *testing.T) {
	cmd := releasecmd.New()

	err := cmd.Run(context.Background(), []string{"release", "verify-changelog"})
	if err == nil || !strings.Contains(err.Error(), `Required flag "changelog-file" not set`) {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyChangelogCmd_SucceedsWhenFileExists(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("CHANGELOG.md", []byte("# Changelog\n\n- entry\n"))

	cmd := releasecmd.New()
	if err := cmd.Run(context.Background(), []string{"release", "verify-changelog", "--changelog-file", "CHANGELOG.md"}); err != nil {
		t.Fatal(err)
	}
}
