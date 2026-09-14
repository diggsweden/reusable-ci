// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolrecorder_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/toolrecorder"
	"github.com/stretchr/testify/require"
)

// The recorder is only worth having if it satisfies the real ports. These
// assertions fail at compile time when a port changes shape, which is the
// moment to update the recorder rather than when one ecosystem's tests drift.
var (
	_ appbuild.GoTool             = (*toolrecorder.Recorder)(nil)
	_ appbuild.CycloneDXGoModTool = (*toolrecorder.Recorder)(nil)
	_ appbuild.CargoTool          = (*toolrecorder.Recorder)(nil)
	_ appbuild.GradleOps          = (*toolrecorder.Recorder)(nil)
	_ appbuild.MavenOps           = (*toolrecorder.Recorder)(nil)
	_ appbuild.NPMOps             = toolrecorder.NPMView{}
	_ appbuild.NPMRunner          = toolrecorder.NPMView{}
	_ appbuild.XcodeBuildOps      = toolrecorder.XcodeView{}
	_ appbuild.XcodeSecurityOps   = toolrecorder.SecurityView{}
)

func TestRecorder_NormalizesEveryPortIntoOneSequence(t *testing.T) {
	t.Parallel()

	rec := toolrecorder.New(t)
	require.NoError(t, rec.Run(t.Context(), domainbuild.GoRunInput{Dir: "svc", Env: []string{"GOFLAGS=-mod=readonly"}, Args: []string{"build"}}))
	require.NoError(t, rec.RunInherit(t.Context(), nil, nil, "clean", "package"))
	require.NoError(t, rec.RunInDirInherit(t.Context(), "app", nil, nil, "assembleRelease"))
	_, _, err := rec.NPM().Run(t.Context(), "pkg", "ci")
	require.NoError(t, err)
	code, err := rec.Xcode().RunInherit(t.Context(), nil, nil, "archive")
	require.NoError(t, err)
	require.Zero(t, code)

	rec.AssertCalls([]toolrecorder.Call{
		{Dir: "svc", Env: []string{"GOFLAGS=-mod=readonly"}, Args: []string{"build"}},
		{Args: []string{"clean", "package"}},
		{Dir: "app", Args: []string{"assembleRelease"}},
		{Dir: "pkg", Args: []string{"ci"}},
		{Args: []string{"archive"}},
	})
}

func TestRecorder_FailsOneCallAndLeavesTheRestAlone(t *testing.T) {
	t.Parallel()

	stop := errors.New("owned tool refusal") //nolint:err113 // per-test cause.
	rec := toolrecorder.New(t).FailAt(1, stop)

	require.NoError(t, rec.RunInherit(t.Context(), nil, nil, "first"))
	require.ErrorIs(t, rec.RunInherit(t.Context(), nil, nil, "second"), stop)
	require.NoError(t, rec.RunInherit(t.Context(), nil, nil, "third"))
	require.Equal(t, [][]string{{"first"}, {"second"}, {"third"}}, rec.Args())
}

func TestRecorder_WritesOutputAndArtifactsWhereThePortExpects(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	rec := toolrecorder.New(t).Respond(func(_ int, call toolrecorder.Call) toolrecorder.Response {
		return toolrecorder.Response{
			Stdout:    "built " + call.Args[0] + "\n",
			Stderr:    "warning\n",
			Artifacts: map[string]string{filepath.Join("dist", "app.tar.gz"): "artifact bytes"},
			ExitCode:  7,
		}
	})

	var stdout, stderr bytes.Buffer

	code, err := rec.Xcode().RunInherit(t.Context(), &stdout, &stderr, "archive")
	require.NoError(t, err)
	require.Equal(t, 7, code, "an exit status is not a failure to start")
	require.Equal(t, "built archive\n", stdout.String())
	require.Equal(t, "warning\n", stderr.String())

	body, readErr := os.ReadFile(fsys.Path(filepath.Join("dist", "app.tar.gz")))
	require.NoError(t, readErr)
	require.Equal(t, "artifact bytes", string(body))
}

func TestRecorder_ArtifactsFollowThePortsDirectory(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	rec := toolrecorder.New(t).Respond(func(int, toolrecorder.Call) toolrecorder.Response {
		return toolrecorder.Response{Artifacts: map[string]string{"target/lib.jar": "jar bytes"}}
	})
	require.NoError(t, rec.RunInDirInherit(t.Context(), "module", nil, nil, "build"))

	body, err := os.ReadFile(fsys.Path(filepath.Join("module", "target", "lib.jar")))
	require.NoError(t, err)
	require.Equal(t, "jar bytes", string(body))
}

func TestRecorder_MavenExpressionIsRecordedInSequence(t *testing.T) {
	t.Parallel()

	rec := toolrecorder.New(t).Respond(func(_ int, call toolrecorder.Call) toolrecorder.Response {
		if len(call.Args) == 2 && call.Args[0] == "--evaluate" {
			return toolrecorder.Response{Stdout: "  1.2.3\n"}
		}

		return toolrecorder.Response{}
	})

	value, err := rec.EvalExpression(t.Context(), "project.version")
	require.NoError(t, err)
	require.Equal(t, "1.2.3", value, "the reader trims what the tool printed")
	require.NoError(t, rec.RunInherit(t.Context(), nil, nil, "package"))
	require.Equal(t, [][]string{{"--evaluate", "project.version"}, {"package"}}, rec.Args())
}
