// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	releasecmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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

// TestPublishCmd_DryRunReconcileNeedsNoForge proves the reconcile-strategy
// preview: on the local platform (no forge) a real publish fails with
// ErrUnsupported, while --dry-run succeeds because the narrating decorator
// replaces the forge publisher — the forge mutation path is never entered.
func TestPublishCmd_DryRunReconcileNeedsNoForge(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("dist/release-notes.md", []byte("notes\n"))
	fsys.WriteFile("dist/asset.tgz", []byte("asset\n"))

	testenv.New(t) // isolated env => local platform, no forge roles

	args := make([]string, 0, 9)
	args = append(args,
		"release", "publish", "--tag", "v1.2.3", "--repository", "owner/repo",
		"--asset", "dist/asset.tgz")

	err := releasecmd.New().Run(context.Background(), append(args, "--dry-run"))
	if err != nil {
		t.Fatalf("dry-run publish should not need a forge: %v", err)
	}

	// Flag off: byte-identical behavior — the forge publisher is required
	// and the local platform cannot provide it.
	err = releasecmd.New().Run(context.Background(), args)
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported without --dry-run on local", err)
	}
}

// TestPublishCmd_DryRunRecreateNeedsNoForge is the recreate-strategy twin.
func TestPublishCmd_DryRunRecreateNeedsNoForge(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("dist/release-notes.md", []byte("notes\n"))

	testenv.New(t)

	args := make([]string, 0, 9)
	args = append(args,
		"release", "publish", "--strategy", "recreate", "--tag", "v1.2.3",
		"--repository", "owner/repo")

	err := releasecmd.New().Run(context.Background(), append(args, "--dry-run"))
	if err != nil {
		t.Fatalf("dry-run recreate publish should not need a forge: %v", err)
	}

	err = releasecmd.New().Run(context.Background(), args)
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported without --dry-run on local", err)
	}
}

// TestPublishCmd_DryRunStillFailsOnMissingAsset proves asset existence and
// manifest resolution still run under --dry-run, so config errors surface.
func TestPublishCmd_DryRunStillFailsOnMissingAsset(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("dist/release-notes.md", []byte("notes\n"))

	testenv.New(t)

	err := releasecmd.New().Run(context.Background(), []string{
		"release", "publish", "--dry-run", "--tag", "v1.2.3",
		"--repository", "owner/repo", "--asset", "dist/missing.tgz",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput for a missing asset in dry-run", err)
	}
}

func TestVerifyChangelogCmd_RequiresFlag(t *testing.T) {
	cmd := releasecmd.New()

	err := cmd.Run(context.Background(), []string{"release", "validate-changelog"})
	if err == nil || !strings.Contains(err.Error(), `Required flag "changelog-file" not set`) {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyChangelogCmd_SucceedsWhenFileExists(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("CHANGELOG.md", []byte("# Changelog\n\n- entry\n"))

	cmd := releasecmd.New()
	if err := cmd.Run(context.Background(), []string{"release", "validate-changelog", "--changelog-file", "CHANGELOG.md"}); err != nil {
		t.Fatal(err)
	}
}
