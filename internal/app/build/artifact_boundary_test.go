// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenNPM struct {
	fakeNPMRunner
	mode string
}

func (f *brokenNPM) RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	if args[0] == "pack" {
		switch f.mode {
		case "missing", "stale":
			_, err := io.WriteString(stdout, `[{"name":"@org/app","version":"1.0.0","filename":"org-app-1.0.0.tgz"}]`)

			return err
		case "multiple":
			var captured bytes.Buffer
			if err := f.fakeNPMRunner.RunInherit(ctx, dir, &captured, stderr, args...); err != nil {
				return err
			}

			item := strings.TrimSuffix(strings.TrimPrefix(captured.String(), "["), "]")
			_, err := io.WriteString(stdout, "["+item+","+item+"]")

			return err
		case "wrong-identity":
			if err := f.fakeNPMRunner.RunInherit(ctx, dir, io.Discard, stderr, args...); err != nil {
				return err
			}

			_, err := io.WriteString(stdout, `[{"name":"@other/app","version":"9.0.0","filename":"org-app-1.0.0.tgz"}]`)

			return err
		case "wrong-archive":
			if err := f.fakeNPMRunner.RunInherit(ctx, dir, stdout, stderr, args...); err != nil {
				return err
			}

			return os.WriteFile(filepath.Join(args[3], "org-app-1.0.0.tgz"), npmTarballBytes("9.0.0"), 0o600)
		}
	}

	if args[0] == "--yes" && f.mode == "missing-sbom" {
		return nil
	}

	if args[0] == "--yes" && f.mode == "malformed-sbom" {
		return os.WriteFile(args[5], []byte(`{"bomFormat":"SPDX","specVersion":"1.6","version":1}`), 0o600)
	}

	return f.fakeNPMRunner.RunInherit(ctx, dir, stdout, stderr, args...)
}

func TestNPMArtifactBoundary_RejectsMissingStaleAndAmbiguousOutputs(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"missing", "stale", "multiple", "wrong-identity", "wrong-archive", "missing-sbom", "malformed-sbom"} {
		dir := newNPMDir(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "org-app-1.0.0.tgz"), []byte("old archive"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bom.json"), []byte("old sbom"), 0o600))

		runner := &brokenNPM{mode: mode}
		err := appbuild.NPMReleaseBuild(t.Context(), &recordingSummarySink{}, runner, runner, output.Annotator{}, io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: strings.HasSuffix(mode, "-sbom")}, SBOMToolVersion: "4.2.1"})
		require.Error(t, err)
		body, err := os.ReadFile(filepath.Join(dir, "org-app-1.0.0.tgz"))
		require.NoError(t, err)
		require.Equal(t, "old archive", string(body))
		body, err = os.ReadFile(filepath.Join(dir, "bom.json"))
		require.NoError(t, err)
		require.Equal(t, "old sbom", string(body))
	}

	dir := newNPMDir(t)
	runner := &fakeNPMRunner{}
	err := appbuild.NPMReleaseBuild(t.Context(), &recordingSummarySink{}, runner, runner, output.Annotator{}, io.Discard, io.Discard, appbuild.NPMReleaseBuildInput{ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true}, SBOMToolVersion: "latest"})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Empty(t, runner.calls)
}

type incompleteCargo struct{ empty bool }

func (f incompleteCargo) Run(_ context.Context, in appbuild.CargoRunInput) error {
	if f.empty && in.Args[0] == "build" {
		path := filepath.Join(in.Dir, "target", cargoTargetTripleForFixture(in.Args), "release", "hello")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}

		return os.WriteFile(path, nil, 0o700) //nolint:gosec // synthetic executable-mode fixture, never executed.
	}

	return nil
}
func cargoTargetTripleForFixture(args []string) string {
	for i, arg := range args {
		if arg == "--target" && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

func TestCargoArtifactBoundary_RejectsStaleAndEmptyBinary(t *testing.T) {
	t.Parallel()

	for _, empty := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "target", "x86_64-unknown-linux-gnu", "release", "hello")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("stale executable"), 0o700)) //nolint:gosec // synthetic executable-mode fixture, never executed.
		err := appbuild.CargoBuildBinaries(t.Context(), incompleteCargo{empty: empty}, io.Discard, io.Discard, appbuild.CargoBuildBinariesInput{Dir: dir, BinaryName: "hello", CrateBinaryName: "hello", Version: "1.0.0", Platforms: "linux/amd64", Commit: strings.Repeat("a", 40)})
		require.Error(t, err)
		_, err = os.Stat(filepath.Join(dir, "dist", "linux-amd64", "hello-linux-amd64"))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}
