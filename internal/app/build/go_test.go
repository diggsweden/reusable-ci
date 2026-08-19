// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeGoTool struct{ calls []appbuild.GoRunInput }

func (f *fakeGoTool) Run(_ context.Context, in appbuild.GoRunInput) error {
	f.calls = append(f.calls, in)

	return nil
}

type fakeCycloneDXGoModTool struct{ calls []appbuild.GoRunInput }

func (f *fakeCycloneDXGoModTool) Run(_ context.Context, in appbuild.GoRunInput) error {
	f.calls = append(f.calls, in)

	return nil
}

func TestGoMetadata_EmitsOutputs(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("go.mod", []byte("module github.com/org/app\n"))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	if err := appbuild.GoMetadata(context.Background(), sink, &out, appbuild.GoMetadataInput{Dir: fsys.Root, RefName: "v1.2.3"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.Single("binary-name"); got != "app" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("binary-name = %q", got)
	}

	if got := sink.Single("version"); got != "1.2.3" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("version = %q", got)
	}

	if got := sink.Single("module"); got != "github.com/org/app" {
		t.Errorf("module = %q", got)
	}

	// The outputs are built as a slice of pairs rather than a map because
	// jsonsink emits in insertion order, so this order is what a `--json`
	// consumer reads. Asserting it is what stops a return to a map, which
	// is the diff-noise the comment on that slice describes.
	wantOrder := []string{"binary-name", "version", "module"}
	if got := sink.Order(); !reflect.DeepEqual(got, wantOrder) {
		t.Errorf("emission order = %q, want %q", got, wantOrder)
	}

	if got := out.String(); got != "Binary: app\nModule: github.com/org/app\nVersion: 1.2.3\n" {
		t.Errorf("out = %q", got)
	}
}

// TestGoMetadata_ResolvesNameAndVersion walks both precedence ladders.
// Only the top rung of each had a test, so ArtifactName -- the middle
// rung, and the only one a workflow sets rather than a person -- was
// never shown to be consulted at all, and "dev" was never shown to be
// the floor.
func TestGoMetadata_ResolvesNameAndVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		in          appbuild.GoMetadataInput
		wantBinary  string
		wantVersion string
	}{
		{
			name:        "explicit inputs outrank everything",
			in:          appbuild.GoMetadataInput{ArtifactName: "artifact", BinaryNameInput: "bin", VersionInput: "2.0.0", RefName: "v1.2.3"},
			wantBinary:  "bin",
			wantVersion: "2.0.0",
		},
		{
			name:        "artifact name is the middle rung",
			in:          appbuild.GoMetadataInput{ArtifactName: "artifact", RefName: "v1.2.3"},
			wantBinary:  "artifact",
			wantVersion: "1.2.3",
		},
		{
			name:        "module base is the last resort",
			in:          appbuild.GoMetadataInput{RefName: "v1.2.3"},
			wantBinary:  "app",
			wantVersion: "1.2.3",
		},
		{
			// resolveGoMetadata says both version inputs are normalised
			// the same way "so callers get identical output regardless of
			// which flag they used". Only the ref-name side was shown.
			name:        "version input is v-stripped like a ref name",
			in:          appbuild.GoMetadataInput{VersionInput: "v2.0.0"},
			wantBinary:  "app",
			wantVersion: "2.0.0",
		},
		{
			name:        "no version anywhere falls to dev",
			in:          appbuild.GoMetadataInput{},
			wantBinary:  "app",
			wantVersion: "dev",
		},
		{
			name:        "blank inputs are skipped, not taken",
			in:          appbuild.GoMetadataInput{BinaryNameInput: "  ", ArtifactName: "artifact", VersionInput: " ", RefName: "v1.2.3"},
			wantBinary:  "artifact",
			wantVersion: "1.2.3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			fsys.WriteFile("go.mod", []byte("module github.com/org/app\n"))

			sink := fakeoutputsink.New(t)

			in := tc.in
			in.Dir = fsys.Root

			if err := appbuild.GoMetadata(context.Background(), sink, &bytes.Buffer{}, in); err != nil {
				t.Fatal(err)
			}

			if got := sink.Single("binary-name"); got != tc.wantBinary {
				t.Errorf("binary-name = %q, want %q", got, tc.wantBinary)
			}

			if got := sink.Single("version"); got != tc.wantVersion {
				t.Errorf("version = %q, want %q", got, tc.wantVersion)
			}
		})
	}
}

// TestGoMetadata_Refusals covers what GoMetadata declines to describe.
// Each case also asserts that nothing reached the sink: a half-emitted
// contract is worse than none, since a consumer reading binary-name
// would proceed on a run that failed.
func TestGoMetadata_Refusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		gomod   string // "" writes no go.mod at all
		in      appbuild.GoMetadataInput
		wantErr error
	}{
		{
			name:    "no go.mod",
			in:      appbuild.GoMetadataInput{},
			wantErr: errs.ErrMissingInput,
		},
		{
			name:    "no module directive",
			gomod:   "go 1.26\n",
			in:      appbuild.GoMetadataInput{},
			wantErr: errs.ErrInvalidConfig,
		},
		{
			// A bare keyword is not a directive: the line is trimmed before
			// the "module " prefix test, so this reaches "directive not
			// found" rather than the empty-path branch below it. That
			// branch is unreachable for the same reason -- see
			// docs/open-questions.md.
			name:    "a bare module keyword is not a directive",
			gomod:   "module \ngo 1.26\n",
			in:      appbuild.GoMetadataInput{},
			wantErr: errs.ErrInvalidConfig,
		},
		{
			// The sinks are line-oriented, so a newline in a value can
			// forge further outputs. ghaoutput refuses these too; this is
			// the earlier refusal, before anything is written.
			name:    "binary name carrying a newline",
			gomod:   "module github.com/org/app\n",
			in:      appbuild.GoMetadataInput{BinaryNameInput: "app\nforged=true"},
			wantErr: errs.ErrUsage,
		},
		{
			name:    "version carrying a carriage return",
			gomod:   "module github.com/org/app\n",
			in:      appbuild.GoMetadataInput{VersionInput: "1.0.0\rforged=true"},
			wantErr: errs.ErrUsage,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			if tc.gomod != "" {
				fsys.WriteFile("go.mod", []byte(tc.gomod))
			}

			sink := fakeoutputsink.New(t)

			in := tc.in
			in.Dir = fsys.Root

			var out bytes.Buffer

			err := appbuild.GoMetadata(context.Background(), sink, &out, in)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}

			if got := sink.Keys(); len(got) != 0 {
				t.Errorf("emitted %q on a refused run", got)
			}

			if out.Len() != 0 {
				t.Errorf("wrote %q on a refused run", out.String())
			}
		})
	}
}

func TestGoTest_RunsWithTags(t *testing.T) {
	t.Parallel()

	tool := &fakeGoTool{}
	if err := appbuild.GoTest(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoTestInput{Dir: "src", BuildTags: "integration"}); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "test -tags integration ./..." {
		t.Errorf("args = %q", got)
	}
}

func TestGoDownload_RunsModDownload(t *testing.T) {
	t.Parallel()

	tool := &fakeGoTool{}
	if err := appbuild.GoDownload(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, "src"); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "mod download" {
		t.Errorf("args = %q", got)
	}
}

func TestGoBuildSBOM_WritesCanonicalPath(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCycloneDXGoModTool{}
	if err := appbuild.GoBuildSBOM(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoBuildSBOMInput{
		Dir:        fsys.Root,
		BinaryName: "app",
	}); err != nil {
		t.Fatal(err)
	}

	if len(tool.calls) != 1 {
		t.Fatalf("calls = %+v", tool.calls)
	}

	if got := strings.Join(tool.calls[0].Args, " "); !strings.Contains(got, filepath.Join(".reusable-ci", "go-build-sbom", "app", "bom.json")) {
		t.Errorf("args = %q", got)
	}

	if _, err := os.Stat(filepath.Join(fsys.Root, ".reusable-ci", "go-build-sbom", "app")); err != nil {
		t.Fatalf("sbom dir missing: %v", err)
	}
}

func TestGoBuildBinaries_BuildsPlatforms(t *testing.T) {
	// Not parallel: SOURCE_DATE_EPOCH is set for this test. It is what makes
	// the ldflags deterministic enough to compare, and it covers the
	// reproducibility path at the same time -- resolveBuildDate documents
	// that two identical invocations produce byte-identical binaries, and
	// nothing exercised it.
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

	fsys := testfs.NewReal(t)

	fysDist := fsys.WriteFile("dist/old", []byte("old"))
	if fysDist == "" {
		t.Fatal("fixture not written")
	}

	tool := &fakeGoTool{}

	var out bytes.Buffer
	if err := appbuild.GoBuildBinaries(context.Background(), tool, &out, &bytes.Buffer{}, appbuild.GoBuildBinariesInput{
		Dir:         fsys.Root,
		BinaryName:  "app",
		BuildTags:   "netgo",
		LDFlags:     "-X main.extra=value",
		MainPackage: "./cmd/app",
		Platforms:   "linux/amd64, windows/arm64",
		Version:     "1.2.3",
		Commit:      "abc123",
	}); err != nil {
		t.Fatal(err)
	}

	if len(tool.calls) != 2 {
		t.Fatalf("calls = %+v", tool.calls)
	}

	// Both calls, both halves. The env was checked for the first platform
	// and the args for the second, so windows/arm64 never had its GOOS and
	// GOARCH asserted, and the first call's flags were never looked at.
	wantLDFlags := "-s -w -X main.version=1.2.3 -X main.commit=abc123 -X main.date=2023-11-14T22:13:20Z -X main.extra=value"

	for i, want := range []struct {
		env []string
		out string
	}{
		{env: []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64"}, out: filepath.Join(fsys.Root, "dist", "linux-amd64", "app-linux-amd64")},
		{env: []string{"CGO_ENABLED=0", "GOOS=windows", "GOARCH=arm64"}, out: filepath.Join(fsys.Root, "dist", "windows-arm64", "app-windows-arm64.exe")},
	} {
		if !reflect.DeepEqual(tool.calls[i].Env, want.env) {
			t.Errorf("call %d env = %q, want %q", i, tool.calls[i].Env, want.env)
		}

		wantArgs := []string{
			"build", "-trimpath", "-buildvcs=false",
			"-ldflags", wantLDFlags,
			"-tags", "netgo",
			"-o", want.out,
			"./cmd/app",
		}
		if !reflect.DeepEqual(tool.calls[i].Args, wantArgs) {
			t.Errorf("call %d args =\n%q\nwant\n%q", i, tool.calls[i].Args, wantArgs)
		}
	}

	if !strings.Contains(out.String(), "Building linux/amd64") {
		t.Errorf("out = %s", out.String())
	}
}

// TestGoBuildBinaries_RejectsInvalidSourceDateEpoch covers the other half of
// the reproducibility input. A value that is not integer seconds is a usage
// error rather than something to fall back from silently -- falling back to
// the clock would produce a binary that looks stamped but is not reproducible.
func TestGoBuildBinaries_RejectsInvalidSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "yesterday")

	fsys := testfs.NewReal(t)
	tool := &fakeGoTool{}

	err := appbuild.GoBuildBinaries(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoBuildBinariesInput{
		Dir:        fsys.Root,
		BinaryName: "app",
		Version:    "1.2.3",
		Platforms:  "linux/amd64",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}

	if len(tool.calls) != 0 {
		t.Errorf("built despite an unusable SOURCE_DATE_EPOCH: %+v", tool.calls)
	}
}

func TestGoBuildBinaries_RejectsInvalidPlatformBeforeRemovingDist(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	old := fsys.WriteFile("dist/old", []byte("old"))

	err := appbuild.GoBuildBinaries(context.Background(), &fakeGoTool{}, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.GoBuildBinariesInput{
		Dir:        fsys.Root,
		BinaryName: "app",
		Version:    "v1.2.3",
		Platforms:  "linux/amd64/v2",
	})
	if err == nil || !strings.Contains(err.Error(), "expected GOOS/GOARCH") {
		t.Fatalf("err = %v", err)
	}

	if _, statErr := os.Stat(old); statErr != nil {
		t.Fatalf("dist was removed before validation: %v", statErr)
	}
}
