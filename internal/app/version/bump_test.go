// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeMavenOps struct {
	args  []string
	dir   string
	err   error
	calls int
}

func TestBump_UnsafeEngineInputRefusedBeforeDiagnostics(t *testing.T) {
	t.Parallel()

	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS, projecttype.Cargo} {
		for _, ver := range []string{"2.0.0\nINJECTED=true", "2.0.0\rINJECTED=true", "2.0.0\x00INJECTED=true", "\n2.0.0", "2.0.0\r\n", "2.0.0\xff"} {
			t.Run(string(pt)+"/version/"+ver, func(t *testing.T) {
				testBumpUnsafeEngineInput(t, pt, ver, "")
			})
		}
	}

	for _, pt := range []projecttype.Type{projecttype.Gradle, projecttype.GradleAndroid, projecttype.XcodeIOS} {
		for _, path := range []string{"release\tversion", "release\nversion", "release\rversion", "release\x00version", ".", "../canary"} {
			t.Run(string(pt)+"/path/"+path, func(t *testing.T) {
				testBumpUnsafeEngineInput(t, pt, "2.0.0", path)
			})
		}
	}
}

func testBumpUnsafeEngineInput(t *testing.T, pt projecttype.Type, ver, override string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "project"), 0o700))

	for path, body := range map[string]string{
		"canary": "untouched\n", "project/gradle.properties": "version=1.0.0\nversionName=1.0.0\nversionCode=7\n",
		"project/Cargo.toml": "[package]\nversion = \"1.0.0\"\n", "project/Cargo.lock": "lock canary\n",
	} {
		writeFile(t, root, path, body)
	}

	if override != "" && override != "." && override != "../canary" && !strings.ContainsRune(override, '\x00') {
		writeFile(t, root, "project/"+override, "version=1.0.0\nversionName=1.0.0\nversionCode=7\nMARKETING_VERSION = 1.0.0\n")
	}

	before := bumpOwnedTree(t, root)
	maven, npm, cargo := &fakeMavenOps{}, &fakeNPMOps{}, &fakeCargoOps{avail: true}

	var out bytes.Buffer

	err := appversion.Bump(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm, Cargo: cargo}, &out, &out, output.NewAnnotator(&out, output.FormatGitHub), appversion.BumpInput{
		ProjectType: pt, Version: ver, WorkingDir: filepath.Join(root, "project"), GradleVersionFile: override, XcconfigFile: override,
	})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Empty(t, out.String())
	require.Equal(t, before, bumpOwnedTree(t, root))
	require.Empty(t, maven.args)
	require.Empty(t, npm.args)
	require.Zero(t, maven.calls)
	require.Zero(t, npm.calls)
	require.Empty(t, cargo.calls)
	require.Zero(t, cargo.availabilityChecks)
}

func TestBump_PreservesNativeVersionForms(t *testing.T) {
	t.Parallel()

	for _, ver := range []string{"2.0.0.Final", "2.0-SNAPSHOT", "RELEASE", "preminor", "from-git"} {
		t.Run(ver, func(t *testing.T) {
			root := t.TempDir()

			maven, npm := &fakeMavenOps{}, &fakeNPMOps{}
			for _, pt := range []projecttype.Type{projecttype.Maven, projecttype.NPM} {
				require.NoError(t, appversion.Bump(t.Context(), appversion.BumpOps{Maven: maven, NPM: npm}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
					ProjectType: pt, Version: "  " + ver + "  ", WorkingDir: root, MavenCLIOpts: []string{"--file=alternate.xml"},
					GradleVersionFile: "unused override", XcconfigFile: "unused override",
				}))
			}

			require.Equal(t, []string{"--file=alternate.xml", "versions:set", "-DnewVersion=" + ver, "-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests"}, maven.args)
			require.Equal(t, []string{"version", ver, "--no-git-tag-version", "--allow-same-version"}, npm.args)
		})
	}
}

func TestBump_EngineSerializationPreservesNonSemverValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pt           projecttype.Type
		file, before string
		ver, want    string
	}{
		{projecttype.Gradle, "gradle.properties", "version=1.0.0\nKEEP=yes\n", "release-$candidate", "version=release-$candidate\nKEEP=yes\n"},
		{projecttype.GradleAndroid, "gradle.properties", "versionName=1.0.0\nversionCode=7\n", "2.0.Final", "versionName=2.0.Final\nversionCode=8\n"},
		{projecttype.XcodeIOS, "versions.xcconfig", "MARKETING_VERSION = 1.0\nKEEP = yes\n", "2.0", "MARKETING_VERSION = 2.0\nKEEP = yes\n"},
		{projecttype.XcodeIOS, "versions.xcconfig", "MARKETING_VERSION = 1.0\nKEEP = yes\n", `2.0\candidate`, "MARKETING_VERSION = 2.0\\candidate\nKEEP = yes\n"},
		{projecttype.Cargo, "Cargo.toml", "[package]\nversion = \"1.0.0\"\n", `release-"quote\path`, "[package]\nversion = \"release-\\\"quote\\\\path\"\n"},
	} {
		t.Run(string(tc.pt), func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, tc.file, tc.before)
			require.NoError(t, appversion.Bump(t.Context(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
				ProjectType: tc.pt, Version: tc.ver, WorkingDir: root,
			}))
			require.Equal(t, tc.want, readTestFile(t, filepath.Join(root, tc.file)))
		})
	}
}

func TestBump_StandaloneLiteralFilenames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		pt           projecttype.Type
		before, want string
	}{
		{"jvm", projecttype.Gradle, "version=1.0.0\nKEEP=yes\n", "version=2.0.0\nKEEP=yes\n"},
		{"android", projecttype.GradleAndroid, "versionName=1.0.0\nversionCode=7\nKEEP=yes\n", "versionName=2.0.0\nversionCode=8\nKEEP=yes\n"},
		{"xcode-existing", projecttype.XcodeIOS, "MARKETING_VERSION = 1.0.0\nKEEP = yes\n", "MARKETING_VERSION = 2.0.0\nKEEP = yes\n"},
		{"xcode-create", projecttype.XcodeIOS, "", "MARKETING_VERSION = 2.0.0\n"},
	} {
		for _, path := range []string{"release version", " release ", "config files/release version", "release\vversion", "release\fversion", "release\u00a0version", "release*version", "release?version", "release[1]version", `release\version`, ":(literal)release", ":(glob)release", ":(exclude)release"} {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0o700))
				writeFile(t, root, "canary", "unchanged\n")

				if tc.before != "" {
					writeFile(t, root, path, tc.before)
				}

				wantTree := bumpOwnedTree(t, root)

				state, exists := wantTree[path]
				if !exists {
					state.mode = 0o644
				}

				state.body = tc.want
				wantTree[path] = state
				require.NoError(t, appversion.Bump(t.Context(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
					ProjectType: tc.pt, Version: "2.0.0", WorkingDir: root, GradleVersionFile: path, XcconfigFile: path,
				}))
				require.Equal(t, wantTree, bumpOwnedTree(t, root))
			})
		}
	}
}

func TestBump_GradleBackslashesAndRetries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		pt           projecttype.Type
		before, want string
	}{
		{"jvm-update", projecttype.Gradle, "version=1.0.0\nKEEP=yes\n", "version=VALUE\nKEEP=yes\n"},
		{"jvm-add", projecttype.Gradle, "KEEP=yes\n", "KEEP=yes\nversion=VALUE\n"},
		{"android-update", projecttype.GradleAndroid, "versionName=1.0.0\nversionCode=7\nKEEP=yes\n", "versionName=VALUE\nversionCode=8\nKEEP=yes\n"},
		{"android-add", projecttype.GradleAndroid, "KEEP=yes\n", "KEEP=yes\nversionName=VALUE\nversionCode=1\n"},
	} {
		for _, value := range []struct{ raw, encoded string }{
			{`2.0.0\`, `2.0.0\\`},
			{`release\path\n\u0041`, `release\\path\\n\\u0041`},
			{`release\\path\\`, `release\\\\path\\\\`},
			{`release-$candidate\`, `release-$candidate\\`},
		} {
			t.Run(tc.name+"/"+value.raw, func(t *testing.T) {
				root := t.TempDir()
				writeFile(t, root, "gradle.properties", tc.before)
				writeFile(t, root, "canary", "unchanged\n")
				wantTree := bumpOwnedTree(t, root)
				state := wantTree["gradle.properties"]
				state.body = strings.ReplaceAll(tc.want, "VALUE", value.encoded)
				wantTree["gradle.properties"] = state

				for range 3 {
					require.NoError(t, appversion.Bump(t.Context(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
						ProjectType: tc.pt, Version: value.raw, WorkingDir: root,
					}))
					require.Equal(t, wantTree, bumpOwnedTree(t, root))
				}
			})
		}
	}
}

func (f *fakeMavenOps) RunInheritIn(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.calls++
	f.dir = dir
	f.args = args

	return f.err
}

type fakeNPMOps struct {
	args  []string
	dir   string
	err   error
	calls int
}

func (f *fakeNPMOps) RunInherit(_ context.Context, dir string, _, _ io.Writer, args ...string) error {
	f.calls++
	f.dir = dir
	f.args = args

	return f.err
}

type fakeCargoOps struct {
	avail              bool
	calls              [][]string
	failOne            bool
	err                error
	availabilityChecks int
}

func (f *fakeCargoOps) Available() bool {
	f.availabilityChecks++

	return f.avail
}
func (f *fakeCargoOps) RunInherit(_ context.Context, _ string, _, _ io.Writer, args ...string) error {
	f.calls = append(f.calls, append([]string{}, args...))
	if f.failOne && len(f.calls) == 1 {
		return errors.New("offline failed") //nolint:err113 // test mock error
	}

	return f.err
}

func TestBump_Maven(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root

	mvn := &fakeMavenOps{}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Maven: mvn}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType:  projecttype.Maven,
		Version:      "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WorkingDir:   dir,
		MavenCLIOpts: []string{"--batch-mode"},
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if mvn.dir != dir {
		t.Errorf("dir = %q, want %q", mvn.dir, dir)
	}

	want := []string{"--batch-mode", "versions:set", "-DnewVersion=1.2.3",
		"-DgenerateBackupPoms=false", "-DprocessAllModules=true", "-DskipTests"}
	if !slices.Equal(mvn.args, want) {
		t.Errorf("args = %v, want %v", mvn.args, want)
	}
}

func TestBump_NPM(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root

	npm := &fakeNPMOps{}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{NPM: npm}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.NPM,
		Version:     "1.2.3",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if npm.dir != dir {
		t.Errorf("dir = %q", npm.dir)
	}

	want := []string{"version", "1.2.3", "--no-git-tag-version", "--allow-same-version"}
	if !slices.Equal(npm.args, want) {
		t.Errorf("args = %v, want %v", npm.args, want)
	}
}

func TestBump_NormalizesSemverTagPrefixBeforeMutation(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	npm := &fakeNPMOps{}

	if err := appversion.Bump(context.Background(), appversion.BumpOps{NPM: npm}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.NPM,
		Version:     "v1.2.3",
		WorkingDir:  fsys.Root,
	}); err != nil {
		t.Fatal(err)
	}

	if got := npm.args[1]; got != "1.2.3" {
		t.Fatalf("npm version = %q, want tag prefix removed", got)
	}
}

func TestBump_GradleJVM_RewritesFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=ignore\nversion=0.1.0\n"))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Gradle,
		Version:     "1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	// Exact bytes, not two substrings. Substring checks cannot see a line
	// duplicated, reordered, or appended, and this file is read back by
	// Gradle: a second `version=` line is a build that resolves the wrong one.
	if got, want := string(fsys.ReadFile("gradle.properties")), "versionName=ignore\nversion=1.0.0\n"; got != want {
		t.Errorf("gradle.properties = %q, want %q", got, want)
	}

	// The rewrite preserves the file's existing mode rather than forcing one.
	// The 0o644 the writer passes applies only when it creates the file, so
	// asserting 0644 here would have been asserting the fixture's own mode;
	// what this pins is that a checkout's permissions survive the bump, since
	// the file goes into the release commit as it is found.
	tightened := filepath.Join(dir, "gradle.properties")
	//nolint:gosec // G302: a deliberately non-default mode is what this test preserves.
	if err := os.Chmod(tightened, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Gradle,
		Version:     "2.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	info, err := os.Stat(tightened)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("the rewrite changed the file mode to %04o, want the original 0640", got)
	}
}

func TestBump_GradleAndroid_RewritesFile(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte("versionName=0.1.0\nversionCode=42\n"))

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.GradleAndroid,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("gradle.properties")
	if !strings.Contains(string(body), "versionName=1.0.0") {
		t.Errorf("missing versionName=1.0.0: %q", body)
	}

	if !strings.Contains(string(body), "versionCode=43") {
		t.Errorf("missing versionCode=43: %q", body)
	}

	if !strings.Contains(out.String(), "Incremented versionCode: 42 → 43") {
		t.Errorf("missing log: %s", out.String())
	}
}

func TestBump_GradleAndroid_NoVersionCodeAddsOne(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("gradle.properties", []byte(""))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.GradleAndroid,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("gradle.properties")
	for _, want := range []string{"versionName=1.0.0", "versionCode=1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %s in: %q", want, body)
		}
	}
}

func TestBump_GradleJVM_FileMissingErrors(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	var stderr bytes.Buffer

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appversion.BumpInput{
		ProjectType: projecttype.Gradle,
		Version:     "1.0.0",
		WorkingDir:  fsys.Root,
	})
	// A missing user-supplied version file is EX_NOINPUT (66), not the
	// unclassified internal-bug default (70).
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want errs.ErrMissingInput", err)
	}

	if !strings.Contains(stderr.String(), "::error::Gradle version file not found") {
		t.Errorf("expected ::error:: line, got: %s", stderr.String())
	}
}

func TestBump_GradleVersionFileMustStayInsideWorkingDirectory(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	outside := fsys.WriteFile("outside.properties", []byte("version=0.1.0\n"))
	project := fsys.MkdirAll("project")

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType:       projecttype.Gradle,
		Version:           "1.0.0",
		WorkingDir:        project,
		GradleVersionFile: "../outside.properties",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if body, readErr := os.ReadFile(outside); readErr != nil || string(body) != "version=0.1.0\n" {
		t.Fatalf("outside file changed = %q, %v", body, readErr)
	}
}

func TestBump_GradleVersionFileRefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	outside := fsys.WriteFile("outside.properties", []byte("version=0.1.0\n"))

	project := fsys.MkdirAll("project")
	if err := os.Symlink(outside, filepath.Join(project, "gradle.properties")); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, &stderr,
		output.NewAnnotator(&stderr, output.FormatGitHub), appversion.BumpInput{
			ProjectType: projecttype.Gradle,
			Version:     "1.0.0",
			WorkingDir:  project,
		})
	// A symlink leaving the project is a refusal, not a missing file. It used
	// to arrive as ErrMissingInput under the annotation "Gradle version file
	// not found" -- describing a file the operator can see with `ls`, and
	// burying the one detail worth surfacing: something in the tree points
	// out of it.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for an escaping symlink", err)
	}

	if errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("an escaping symlink still reports as a missing file: %v", err)
	}

	if !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("err = %v, want it to say the path escapes the project", err)
	}

	// And the operator-facing annotation says so too, rather than "not found".
	if got := stderr.String(); !strings.Contains(got, "resolves outside the project directory") {
		t.Errorf("annotation = %q, want it to name the escape", got)
	}

	if body, readErr := os.ReadFile(outside); readErr != nil || string(body) != "version=0.1.0\n" {
		t.Fatalf("outside symlink target changed = %q, %v", body, readErr)
	}
}

func TestBump_XcodeIOS_CreatesFileWhenMissing(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType:  projecttype.XcodeIOS,
		Version:      "1.2.3",
		WorkingDir:   fsys.Root,
		XcconfigFile: "versions.xcconfig",
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("versions.xcconfig")
	if string(body) != "MARKETING_VERSION = 1.2.3\n" {
		t.Errorf("body = %q", body)
	}
}

func TestBump_XcodeIOS_UpdatesExisting(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("versions.xcconfig", []byte("MARKETING_VERSION = 0.0.1\nFOO = bar\n"))

	if err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.XcodeIOS,
		Version:     "1.2.3",
		WorkingDir:  fsys.Root,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("versions.xcconfig")
	if !strings.Contains(string(body), "MARKETING_VERSION = 1.2.3") {
		t.Errorf("body = %q", body)
	}

	if !strings.Contains(string(body), "FOO = bar") {
		t.Errorf("must not touch other keys: %q", body)
	}
}

func TestBump_Cargo_Workspace(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	cargo := `[workspace.package]
version = "0.0.1"
edition = "2024"

[dependencies]
serde = "1"
`
	fsys.WriteFile("Cargo.toml", []byte(cargo))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	body := fsys.ReadFile("Cargo.toml")
	if !strings.Contains(string(body), `version = "1.0.0"`) {
		t.Errorf("body = %s", body)
	}
}

func TestBump_Cargo_RefreshesLockWhenPresent(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	cargoToml := `[package]
name = "demo"
version = "0.0.1"
`
	fsys.WriteFile("Cargo.toml", []byte(cargoToml))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if len(ops.calls) == 0 {
		t.Fatal("expected cargo update to be invoked")
	}

	want := []string{"update", "--workspace", "--offline"}
	if !slices.Equal(ops.calls[0], want) {
		t.Errorf("first cargo call = %v, want %v", ops.calls[0], want)
	}
}

func TestBump_Cargo_OfflineFailureFallsBackToOnline(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: true, failOne: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	// The name is the claim: counting the calls cannot tell a retry that
	// dropped --offline from one that repeated the same offline command and
	// failed again.
	want := [][]string{
		{"update", "--workspace", "--offline"},
		{"update", "--workspace"},
	}
	if !slices.EqualFunc(ops.calls, want, slices.Equal) {
		t.Errorf("cargo calls = %v, want %v", ops.calls, want)
	}
}

func TestBump_Cargo_MissingManifestIsMissingInput(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: &fakeCargoOps{avail: true}}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  fsys.Root, // no Cargo.toml written
	})
	// A missing user-supplied Cargo.toml is EX_NOINPUT (66), not the
	// unclassified internal-bug default (70).
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want errs.ErrMissingInput", err)
	}
}

func TestBump_Cargo_NoLockSkipsRefresh(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))

	ops := &fakeCargoOps{avail: true}
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if len(ops.calls) != 0 {
		t.Errorf("expected no cargo calls when Cargo.lock missing, got %d", len(ops.calls))
	}
}

func TestBump_Cargo_NotInstalledWarnsButSucceeds(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.WriteFile("Cargo.toml", []byte("[package]\nname = \"x\"\nversion = \"0.0.1\"\n"))
	fsys.WriteFile("Cargo.lock", []byte("# stub"))

	ops := &fakeCargoOps{avail: false}

	var out bytes.Buffer
	if err := appversion.Bump(context.Background(), appversion.BumpOps{Cargo: ops}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Cargo,
		Version:     "1.0.0",
		WorkingDir:  dir,
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}

	if !strings.Contains(out.String(), "Warning: cargo not found") {
		t.Errorf("expected warning, got: %s", out.String())
	}
}

// TestBump_NoOpProjectTypesTouchNothing covers the claim the two no-op tests
// made in their names and did not check.
//
// Both asserted the message and nothing else, so a "no-op" that wrote a
// version file, or rewrote one that was already there, passed. Go and meta
// projects carry their version in the tag rather than in a tracked file, and
// the bump step runs against a checkout that is about to be committed: a file
// written here is a spurious diff in the release commit.
//
// The canary is a working directory seeded with the files these paths would
// plausibly touch, compared byte-for-byte and mode-for-mode afterwards.
func TestBump_NoOpProjectTypesTouchNothing(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		projectType projecttype.Type
		wantInOut   []string
	}{
		"meta": {projectType: projecttype.Meta, wantInOut: []string{"Meta project type"}},
		"go":   {projectType: projecttype.Go, wantInOut: []string{"Go project type", "1.2.3"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)

			seeded := map[string]string{
				"gradle.properties": "version=0.1.0\n",
				"package.json":      `{"version":"0.1.0"}`,
				"VERSION":           "0.1.0\n",
			}

			for path, body := range seeded {
				fsys.WriteFile(path, []byte(body))
			}

			before := treeSnapshot(t, fsys.Root)

			var out bytes.Buffer
			if err := appversion.Bump(context.Background(), appversion.BumpOps{}, &out, io.Discard, output.Annotator{}, appversion.BumpInput{
				ProjectType: tc.projectType,
				Version:     "1.2.3",
				WorkingDir:  fsys.Root,
			}); err != nil {
				t.Fatalf("Bump: %v", err)
			}

			for _, want := range tc.wantInOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the no-op notice does not mention %q:\n%s", want, out.String())
				}
			}

			if after := treeSnapshot(t, fsys.Root); !maps.Equal(before, after) {
				t.Errorf("a no-op bump changed the working tree:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// treeSnapshot maps every regular file under root to its mode and contents, so
// a comparison catches a changed byte, a changed mode, a new file and a
// removed one alike.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()

	snapshot := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // test fixture under t.TempDir().
		if readErr != nil {
			return readErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		snapshot[rel] = fmt.Sprintf("%04o:%s", info.Mode().Perm(), body)

		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}

	return snapshot
}

func TestBump_UnknownTypeErrors(t *testing.T) {
	t.Parallel()

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: "java",
		Version:     "1.0.0",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "unknown project type") {
		t.Errorf("err = %v, want it to name the rejected type", err)
	}
}

func TestBump_RequiresVersion(t *testing.T) {
	t.Parallel()

	err := appversion.Bump(context.Background(), appversion.BumpOps{}, io.Discard, io.Discard, output.Annotator{}, appversion.BumpInput{
		ProjectType: projecttype.Maven,
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "version is required") {
		t.Errorf("err = %v, want it to name the missing flag", err)
	}
}
