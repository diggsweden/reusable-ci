// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeNPMOps struct {
	out    string
	stderr string
	err    error
}

func (f fakeNPMOps) Run(context.Context, string, ...string) (string, string, error) {
	return f.out, f.stderr, f.err
}

func TestNPMMetadata_EmitsOutputs(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"@org/app","version":"1.2.3"}`))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appbuild.NPMMetadata(context.Background(), sink, &out, output.Annotator{}, appbuild.NPMMetadataInput{Dir: fsys.Root, PackageScope: "@org"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("name"); got != "@org/app" {
		t.Errorf("name = %q", got)
	}

	if got := sink.Single("version"); got != "1.2.3" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q", got)
	}

	if !strings.Contains(out.String(), "@org/app@1.2.3") {
		t.Errorf("out = %s", out.String())
	}
}

func TestNPMMetadata_RejectsWrongScope(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"app","version":"1.2.3"}`))

	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := appbuild.NPMMetadata(context.Background(), sink, &bytes.Buffer{}, output.NewAnnotator(&stderr, output.FormatGitHub), appbuild.NPMMetadataInput{Dir: fsys.Root, PackageScope: "@org"})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "package.json name must be scoped") {
		t.Errorf("stderr = %s", stderr.String())
	}
}

func TestNPMPack_EmitsTarballOutput(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("app-1.2.3.tgz", []byte("tarball"))

	sink := fakeoutputsink.New(t)

	err := appbuild.NPMPack(context.Background(), fakeNPMOps{out: `[{"filename":"app-1.2.3.tgz"}]`}, sink, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMMetadataInput{Dir: fsys.Root})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("tarball"); got != "app-1.2.3.tgz" {
		t.Errorf("tarball = %q", got)
	}
}

func TestNPMPack_RejectsAmbiguousPackOutput(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	err := appbuild.NPMPack(context.Background(), fakeNPMOps{out: `[]`}, sink, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMMetadataInput{Dir: fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "want exactly 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestNPMPack_PropagatesNPMFailure(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := appbuild.NPMPack(context.Background(), fakeNPMOps{stderr: "boom", err: errors.New("npm failed")}, sink, &bytes.Buffer{}, &stderr, appbuild.NPMMetadataInput{Dir: fsys.Root}) //nolint:err113 // test mock error
	if err == nil {
		t.Fatal("expected error")
	}

	if stderr.String() != "boom" {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestNPMPack_RequiresCreatedFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := fakeoutputsink.New(t)

	err := appbuild.NPMPack(context.Background(), fakeNPMOps{out: `[{"filename":"missing.tgz"}]`}, sink, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMMetadataInput{Dir: fsys.Root})
	if err == nil || !os.IsNotExist(errors.Unwrap(err)) {
		// The exact wrapping includes context; checking text keeps this stable.
		if err == nil || !strings.Contains(err.Error(), "missing.tgz") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestNPMPack_UsesWorkingDirectory(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("pkg", "app.tgz"), []byte("tarball"))

	sink := fakeoutputsink.New(t)

	err := appbuild.NPMPack(context.Background(), fakeNPMOps{out: `[{"filename":"app.tgz"}]`}, sink, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMMetadataInput{Dir: fsys.Path("pkg")})
	if err != nil {
		t.Fatal(err)
	}
}

type recordingNPMRunner struct {
	dir    string
	args   []string
	runErr error
}

func (r *recordingNPMRunner) RunInherit(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	r.dir = dir
	r.args = args

	return r.runErr
}

func TestNPMApplication_RunsBuildScriptWhenPresent(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"x","version":"1.0.0","scripts":{"build":"tsc"}}`))

	runner := &recordingNPMRunner{}

	var out bytes.Buffer
	if err := appbuild.NPMApplication(context.Background(), runner, &out, &bytes.Buffer{}, appbuild.NPMApplicationInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := runner.args; len(got) != 2 || got[0] != "run" || got[1] != "build" {
		t.Errorf("args = %v, want [run build]", got)
	}

	if !strings.Contains(out.String(), `Running "build" npm script`) {
		t.Errorf("missing run log: %s", out.String())
	}
}

func TestNPMApplication_SkipsWhenScriptAbsent(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"x","version":"1.0.0","scripts":{"test":"tap"}}`))

	runner := &recordingNPMRunner{}

	var out bytes.Buffer
	if err := appbuild.NPMApplication(context.Background(), runner, &out, &bytes.Buffer{}, appbuild.NPMApplicationInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if runner.args != nil {
		t.Errorf("npm should not have run, got args %v", runner.args)
	}

	if !strings.Contains(out.String(), "skipping build step") {
		t.Errorf("missing skip log: %s", out.String())
	}
}

func TestNPMApplication_SkipsWhenScriptEmpty(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`{"name":"x","version":"1.0.0","scripts":{"build":"   "}}`))

	runner := &recordingNPMRunner{}
	if err := appbuild.NPMApplication(context.Background(), runner, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMApplicationInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if runner.args != nil {
		t.Errorf("whitespace-only script body should be treated as absent")
	}
}

func TestNPMApplication_RejectsInvalidPackageJSON(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("package.json", []byte(`not json`))

	err := appbuild.NPMApplication(context.Background(), &recordingNPMRunner{}, io.Discard, io.Discard, appbuild.NPMApplicationInput{Dir: fsys.Root})
	if err == nil || !strings.Contains(err.Error(), "parse package.json") {
		t.Fatalf("err = %v", err)
	}
}
