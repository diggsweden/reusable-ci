// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
)

// runPlanWrite runs `reusable-ci plan write` through the real root so
// scope validation walks the real command tree.
func runPlanWrite(t *testing.T, args ...string) error {
	t.Helper()

	root := cli.New(cli.BuildInfo{Version: "dev"})

	return root.Run(context.Background(), append([]string{"reusable-ci", "plan", "write"}, args...))
}

func readPlan(t *testing.T, path string) map[string]map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path) //nolint:gosec // test reads its own temp file.
	require.NoError(t, err)

	var plan map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &plan))

	return plan
}

func TestPlanWrite_CreatesAndMergesScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "context=.",
		"--set", "platform=linux/amd64"))

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "release assemble-dist",
		"--set", "path=dist/"))

	// A second write to an existing scope merges instead of replacing.
	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "mode=push-by-digest"))

	plan := readPlan(t, path)
	require.Equal(t, ".", plan["container build"]["context"])
	require.Equal(t, "linux/amd64", plan["container build"]["platform"])
	require.Equal(t, "push-by-digest", plan["container build"]["mode"])
	require.Equal(t, "dist/", plan["release assemble-dist"]["path"])
}

func TestPlanWrite_RejectsUnknownScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")

	err := runPlanWrite(t, "--plan-file", path, "--scope", "container biuld", "--set", "context=.")
	require.ErrorContains(t, err, "does not resolve to a command")

	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "no plan file may be written on a failed validation")
}

func TestPlanWrite_RejectsGroupScope(t *testing.T) {
	err := runPlanWrite(t, "--plan-file", filepath.Join(t.TempDir(), "p.json"),
		"--scope", "container", "--set", "context=.")
	require.ErrorContains(t, err, "not a leaf command")
}

func TestPlanWrite_RejectsUnknownKey(t *testing.T) {
	err := runPlanWrite(t, "--plan-file", filepath.Join(t.TempDir(), "p.json"),
		"--scope", "container build", "--set", "contxt=.")
	require.ErrorContains(t, err, `"contxt" is not a flag of "container build"`)
}

func TestPlanWrite_RejectsMalformedSet(t *testing.T) {
	err := runPlanWrite(t, "--plan-file", filepath.Join(t.TempDir(), "p.json"),
		"--scope", "container build", "--set", "no-equals-sign")
	require.ErrorContains(t, err, "is not flag=value")
}

// TestPlanWrite_ProducesAFileThatPlanfileCanRead closes the seam between the
// writer and the reader.
//
// planfile's own tests are thorough, but every one of them hand-writes the plan
// JSON it then reads back, so they all agree with each other by construction.
// Nothing checked that what `plan write` actually emits is what planfile
// actually consumes -- if the writer changed its scope key or nesting, those
// tests would stay green and the feature would be broken end to end.
//
// So this writes through the real command and resolves through planfile's real
// flag source, using the same planfile.Vars wiring a production flag uses.
//
// It replaces a version that set REUSABLE_CI_PLAN, never read it, and then
// asserted that the JSON it had just written contained what it wrote -- a
// tautology already covered by TestPlanWrite_CreatesAndMergesScopes, under a
// doc comment claiming it proved consumption.
func TestPlanWrite_ProducesAFileThatPlanfileCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "context=from-the-plan"))

	t.Setenv(planfile.EnvVar, path)

	var got string

	// The env-var source is deliberately a name nothing sets: if the value
	// arrives, it can only have come from the plan file.
	probe := &urfavecli.Command{
		Name: "probe",
		Flags: []urfavecli.Flag{&urfavecli.StringFlag{
			Name:    "context",
			Sources: planfile.Vars("container build", "context", "PLAN_WRITE_ROUNDTRIP_UNSET"),
		}},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			got = cmd.String("context")

			return nil
		},
	}
	require.NoError(t, probe.Run(context.Background(), []string{"probe"}))

	require.Equal(t, "from-the-plan", got,
		"a plan written by `plan write` did not resolve through planfile: "+
			"the writer's scope key or value shape no longer matches the reader's")
}

// TestPlanWrite_ReplacesASymlinkedDestinationInsteadOfWritingThroughIt covers
// where the plan actually lands.
//
// The plan file holds whatever `--set` values the caller passed, and those can
// come from a secret store. Writing it with os.WriteFile FOLLOWED a symlink at
// the plan path, so anything able to place one — a stale run, a shared
// workspace, a checked-in link — redirected the write to a destination the
// operator never named, and the command reported success. Replacing the link is
// the fix rather than refusing: the write lands at the path that was asked for,
// and the link's target is left alone.
func TestPlanWrite_ReplacesASymlinkedDestinationInsteadOfWritingThroughIt(t *testing.T) {
	dir := t.TempDir()

	// Valid JSON, because `plan write` merges the existing plan before
	// writing: the point under test is where the WRITE lands, not what the
	// merge reads.
	const original = "{\n  \"must not be touched\": {}\n}\n"

	outside := filepath.Join(dir, "outside.json")
	require.NoError(t, os.WriteFile(outside, []byte(original), 0o600))

	path := filepath.Join(dir, "plan.json")
	require.NoError(t, os.Symlink(outside, path))

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "context=from-the-plan"))

	// The link's target is untouched.
	body, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.JSONEq(t, original, string(body), "the plan was written through the symlink")

	// The plan path is now a regular file holding the plan.
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "plan path is still a symlink: %s", info.Mode())

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(written), "from-the-plan")
}

// TestPlanWrite_TightensAnExistingLoosePermission covers the mode.
//
// os.WriteFile applies its perm argument only when it creates the file, so a
// plan overwritten at a path that already existed as 0644 stayed world-readable
// however tightly the write was specified. That is the same file that can carry
// secret-store values, sitting in a workspace other jobs on the runner can read.
func TestPlanWrite_TightensAnExistingLoosePermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	//nolint:gosec // G306: a deliberately loose mode; the test exists to prove it is tightened.
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "context=from-the-plan"))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"an existing plan file kept its loose permissions")
}

// TestPlanWrite_LeavesNoTemporaryFileBehind is the other half of writing
// through a rename: the sibling temporary must not survive, or a workspace
// accumulates readable copies of every plan ever written.
func TestPlanWrite_LeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")

	require.NoError(t, runPlanWrite(t,
		"--plan-file", path,
		"--scope", "container build",
		"--set", "context=from-the-plan"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	require.Equal(t, []string{"plan.json"}, names, "a temporary file survived the write")
}
