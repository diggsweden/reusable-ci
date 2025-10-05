// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

// What happens to a file a native tool writes that the plan never declared?
//
// It is not a hypothetical. The engine itself calls `cargo update --workspace`
// after a Cargo bump (refreshCargoLock in bump.go), and `npm version` rewrites
// package-lock.json. The bump preflight's inventory — primary version files,
// changelog sources, changelog destinations — contains none of these. Two
// answers were refused when this was written down:
//
//   - Model each tool's outputs, so the inventory is complete. That means
//     reimplementing configuration resolution per toolchain: which lockfile a
//     Cargo workspace writes, which modules a reactor touches, where npm
//     workspace roots are. A wrong model is worse than none — it would claim
//     files the tool did not write and miss ones it did.
//   - Revert them, so the tree matches the plan. That is a rollback promise the
//     engine cannot keep; a failure can land between the write and the revert.
//
// The answer actually in force is neither, and it lives in
// internal/domain/version/filepattern.go: the native refresh outputs a project
// type is KNOWN to produce are declared, per type, in that type's commit
// pathspec. Cargo's pattern names Cargo.lock; npm's names package-lock.json. So
// those files are committed on purpose — which is the only correct answer here,
// because internal/app/validate/cargo.go refuses a release whose Cargo.lock is
// not committed.
//
// The gap that made this a defect is that the two lists are maintained apart
// and nothing compared them. FilePattern is a switch in domain; the refresh is
// a call in app. A project type could gain a native refresh whose output no
// pattern names, and the file would sit in the working tree, out of the release
// commit, with nothing failing. This test pins the pairing in both directions.
func TestBumpPlan_NativeRefreshOutputsAreDeclaredInTheCommitPattern(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	const manifest = "[package]\nname = \"fixture\"\nversion = \"1.0.0\"\n"

	for path, body := range map[string]string{
		"Cargo.toml":        manifest,
		"Cargo.lock":        "version = 3\n# before the bump\n",
		"package.json":      `{"name":"fixture","version":"1.0.0"}` + "\n",
		"package-lock.json": `{"lockfileVersion":3,"note":"before the bump"}` + "\n",
		"full.md":           "# changes\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o600))
	}

	// These fakes do what the real tools do: write a file the preflight
	// inventory never modelled.
	cargo := &lockWritingOps{root: root, path: "Cargo.lock", body: "version = 3\n# refreshed by cargo update\n", avail: true}
	npm := &lockWritingOps{root: root, path: "package-lock.json", body: `{"lockfileVersion":3,"note":"refreshed by npm version"}` + "\n"}

	in := aliasPlanInput(t, []pipeline.PlannedArtifact{
		{Name: "rust", ProjectType: projecttype.Cargo, WorkingDirectory: "."},
		{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "."},
	}, "")

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	require.NoError(t, appversion.BumpPlan(t.Context(),
		appversion.BumpOps{Maven: &fakeMavenOps{}, NPM: npm, Cargo: cargo},
		sink, &out, &out, output.Annotator{}, in))

	require.Equal(t, 1, cargo.writes, "the fixture must actually have written a lockfile, or this test proves nothing")
	require.Equal(t, 1, npm.writes)

	// Not reverted: whatever the tool wrote is what is there.
	require.Equal(t, "version = 3\n# refreshed by cargo update\n", readTreeFile(t, root, "Cargo.lock"))
	require.JSONEq(t, `{"lockfileVersion":3,"note":"refreshed by npm version"}`, readTreeFile(t, root, "package-lock.json"))

	// Declared: the pattern the release commit stages names them, so the
	// refreshed lockfile ships with the release that refreshed it.
	staged := strings.Fields(sink.Single("file-pattern"))
	require.NotEmpty(t, staged, "a bump that changed files must emit the pattern that stages them")

	for _, refreshed := range []string{"Cargo.lock", "package-lock.json", "Cargo.toml", "package.json"} {
		require.Containsf(t, staged, refreshed,
			"%s is written during the bump but not staged by it; the release commit would omit a file the release depends on", refreshed)
	}
}

// TestBumpPlan_UndeclaredNativeOutputsStayOutOfTheCommit is the other half of
// the same decision, and the honest one.
//
// Only the outputs a project type is known to produce are declared. Anything
// else a tool writes — an npm workspace's nested lockfile, a module the reactor
// reached that the pattern's glob does not cover — is left where it is and is
// not staged. That is the supported behaviour and the reason it is safe: the
// release commit contains what the plan named, never more.
//
// The cost is stated rather than hidden. A run that needs such a file in the
// release commit has to declare it as a custom version file; it does not arrive
// by accident.
func TestBumpPlan_UndeclaredNativeOutputsStayOutOfTheCommit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	require.NoError(t, os.Mkdir(filepath.Join(root, "packages"), 0o700))

	for path, body := range map[string]string{
		"package.json":               `{"name":"fixture","version":"1.0.0"}` + "\n",
		"package-lock.json":          `{"lockfileVersion":3}` + "\n",
		"packages/package-lock.json": "# a workspace member's lockfile\n",
		"full.md":                    "# changes\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o600))
	}

	npm := &lockWritingOps{root: root, path: "packages/package-lock.json", body: "# refreshed by the workspace-aware tool\n"}

	in := aliasPlanInput(t, []pipeline.PlannedArtifact{
		{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "."},
	}, "")

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	require.NoError(t, appversion.BumpPlan(t.Context(),
		appversion.BumpOps{Maven: &fakeMavenOps{}, NPM: npm, Cargo: &lockWritingOps{}},
		sink, &out, &out, output.Annotator{}, in))

	require.Equal(t, 1, npm.writes)
	require.Equal(t, "# refreshed by the workspace-aware tool\n", readTreeFile(t, root, "packages/package-lock.json"),
		"an undeclared output is left alone, not reverted")

	staged := strings.Fields(sink.Single("file-pattern"))
	require.NotContains(t, staged, "packages/package-lock.json",
		"an undeclared output must not be staged; the release commit names only what the plan declared")
	require.Contains(t, staged, "package-lock.json", "the declared root lockfile is still staged")
}

// lockWritingOps stands in for a native tool that refreshes a lockfile the plan
// never declared, which is what cargo update and npm version really do.
type lockWritingOps struct {
	root   string
	path   string
	body   string
	avail  bool
	writes int
	err    error
}

func (f *lockWritingOps) Available() bool { return f.avail }

func (f *lockWritingOps) RunInherit(_ context.Context, _ string, _, _ io.Writer, _ ...string) error {
	if f.err != nil {
		return f.err
	}

	if f.writes > 0 {
		return nil
	}

	f.writes++

	if err := os.WriteFile(filepath.Join(f.root, f.path), []byte(f.body), 0o600); err != nil {
		return errors.New("fixture could not write " + f.path) //nolint:err113 // test fixture.
	}

	return nil
}

func readTreeFile(t *testing.T, root, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // owned temporary tree.
	require.NoError(t, err)

	return string(body)
}
