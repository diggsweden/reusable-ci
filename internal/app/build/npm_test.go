// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if !strings.Contains(stderr.String(), "package.json name must be scoped") {
		t.Errorf("stderr = %s", stderr.String())
	}

	// The scope decides where this would be published, so a mismatch must
	// not leave a name and version behind for a later step to act on.
	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q despite the scope mismatch", got)
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

// TestNPMPack_RejectsUnusablePackOutput covers every way the pack output
// can fail to name exactly one tarball. The previous test was called
// RejectsAmbiguousPackOutput and fed it "[]" -- the empty case, not the
// ambiguous one -- so the branch its name described was never run.
func TestNPMPack_RejectsUnusablePackOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		out  string
	}{
		{name: "no packages", out: `[]`},
		{
			// The case the old name promised: npm packed a workspace and
			// returned several, so which one to publish is not decidable.
			name: "several packages",
			out:  `[{"filename":"a-1.0.0.tgz"},{"filename":"b-1.0.0.tgz"}]`,
		},
		{name: "a package with no filename", out: `[{}]`},
		{name: "not json at all", out: `npm ERR! code ENOENT`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			sink := fakeoutputsink.New(t)

			err := appbuild.NPMPack(context.Background(), fakeNPMOps{out: tc.out}, sink, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.NPMMetadataInput{Dir: fsys.Root})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if got := sink.Keys(); len(got) != 0 {
				t.Errorf("emitted %q for an unusable pack output", got)
			}
		})
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

	// npm reported a tarball that is not on disk. errors.Is walks the
	// chain, so the wrapping context needs no special handling -- the
	// previous nested form could pass without ever reaching a claim.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a not-exist error", err)
	}

	if !strings.Contains(err.Error(), "missing.tgz") {
		t.Errorf("err = %v, want it to name the missing tarball", err)
	}

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q for a tarball that does not exist", got)
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

func TestNPMApplication_RejectsUnreadablePackageJSON(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		body    string // "" writes no package.json
		wantErr error
	}{
		{name: "not json", body: `not json`, wantErr: errs.ErrInvalidConfig},
		{name: "truncated json", body: `{"name":"x"`, wantErr: errs.ErrInvalidConfig},
		{
			// fs.ErrNotExist, not ErrMissingInput: npmHasScript returns the
			// raw read error while its sibling readNPMPackageJSON maps the
			// same condition to ErrMissingInput, so the exit code depends
			// on which npm subcommand was run. Recorded in
			// docs/open-questions.md; pinned here as it behaves today.
			name:    "no package.json",
			wantErr: fs.ErrNotExist,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			if tc.body != "" {
				fsys.WriteFile("package.json", []byte(tc.body))
			}

			runner := &recordingNPMRunner{}

			err := appbuild.NPMApplication(context.Background(), runner, io.Discard, io.Discard, appbuild.NPMApplicationInput{Dir: fsys.Root})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			// A package.json that cannot be read is not the same as one
			// declaring no build script: the first must refuse, the second
			// skips. Only the skip may reach a successful run, and neither
			// may invoke npm.
			if runner.args != nil {
				t.Errorf("ran npm despite an unreadable package.json: %v", runner.args)
			}
		})
	}
}
