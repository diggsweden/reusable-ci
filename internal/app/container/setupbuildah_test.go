// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeBuildahSetupTool struct {
	commands           map[string]bool
	failOverlayProbe   bool
	probeCalls         []string
	probeContainerfile string
	probeDir           string
	// calls records RemoveImage/ProbeBuild in the order they arrive, so a
	// probe that could pass on a leftover image is visible.
	calls    []string
	infoJSON []byte
}

func (f *fakeBuildahSetupTool) CommandExists(name string) bool {
	return f.commands[name]
}

func (f *fakeBuildahSetupTool) InfoDriver(_ context.Context, env []string) (string, error) {
	return storageDriverFromEnv(env)
}

func (f *fakeBuildahSetupTool) InfoJSON(_ context.Context, _ []string) ([]byte, error) {
	if len(f.infoJSON) == 0 {
		return []byte(`{"store":{"GraphDriverName":"vfs"}}`), nil
	}

	return f.infoJSON, nil
}

func (f *fakeBuildahSetupTool) ProbeBuild(_ context.Context, env []string, probeDir, _ string, _ io.Writer) error {
	driver, err := storageDriverFromEnv(env)
	if err != nil {
		return err
	}

	if body, readErr := os.ReadFile(filepath.Join(probeDir, "Containerfile")); readErr == nil {
		f.probeContainerfile = string(body)
	}

	f.probeDir = probeDir
	f.calls = append(f.calls, "build")
	f.probeCalls = append(f.probeCalls, driver)

	if driver == "overlay" && f.failOverlayProbe {
		return errors.New("simulated overlay probe failure") //nolint:err113 // test double error.
	}

	return nil
}

func (f *fakeBuildahSetupTool) RemoveImage(_ context.Context, _ []string, image string, _ io.Writer) error {
	f.calls = append(f.calls, "remove:"+image)

	return nil
}

type fakePackageInstaller struct {
	packages []string
}

func (f *fakePackageInstaller) Install(_ context.Context, packages []string, _ io.Writer) error {
	f.packages = append([]string{}, packages...)

	return nil
}

type fakeSummarySink struct {
	body string
}

func (f *fakeSummarySink) Append(_ context.Context, markdown string) error {
	f.body += markdown

	return nil
}

// TestSetupBuildah_ChoosesTheDriverItsProbeAccepts covers driver selection in
// both directions: overlay is tried first and kept when a probe build works,
// and vfs is the fallback when it does not.
//
// Only the fallback was covered. A change that always ended up on vfs would
// have passed, and nothing would report it -- vfs is the slow path, so the
// symptom is builds quietly taking longer rather than failing.
func TestSetupBuildah_ChoosesTheDriverItsProbeAccepts(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		failOverlayProbe bool
		wantDriver       string
		wantProbes       []string
	}{
		"overlay works":       {failOverlayProbe: false, wantDriver: "overlay", wantProbes: []string{"overlay"}},
		"overlay probe fails": {failOverlayProbe: true, wantDriver: "vfs", wantProbes: []string{"overlay", "vfs"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scratch := t.TempDir()
			tool := &fakeBuildahSetupTool{
				commands: map[string]bool{
					"buildah":        true,
					"fuse-overlayfs": true,
				},
				failOverlayProbe: testCase.failOverlayProbe,
			}
			sink := fakeoutputsink.New(t)
			summary := &fakeSummarySink{}
			envFile := filepath.Join(scratch, "env")

			got, err := appcontainer.SetupBuildah(context.Background(), tool, nil, sink, summary, io.Discard, appcontainer.SetupBuildahInput{
				InstallPackages: false,
				ProbeBuild:      true,
				WriteSummary:    true,
				StorageConf:     filepath.Join(scratch, "storage.conf"),
				StorageRoot:     filepath.Join(scratch, "root"),
				TmpDir:          filepath.Join(scratch, "tmp"),
				RunnerTemp:      scratch,
				EnvFile:         envFile,
			})
			if err != nil {
				t.Fatal(err)
			}

			if got.Driver != testCase.wantDriver {
				t.Errorf("driver = %q, want %q", got.Driver, testCase.wantDriver)
			}

			// Overlay is always tried first; vfs only appears after it fails.
			if !reflect.DeepEqual(tool.probeCalls, testCase.wantProbes) {
				t.Errorf("probe calls = %v, want %v", tool.probeCalls, testCase.wantProbes)
			}

			// The driver buildah will actually use comes from this file, not
			// from the returned value.
			if body := readText(t, got.StorageConf); !strings.Contains(body, `driver = "`+testCase.wantDriver+`"`) {
				t.Errorf("storage conf does not select %s:\n%s", testCase.wantDriver, body)
			}

			env := readText(t, envFile)
			for _, want := range []string{
				"CONTAINERS_STORAGE_CONF=" + got.StorageConf,
				"TMPDIR=" + got.TmpDir,
			} {
				if !strings.Contains(env, want) {
					t.Errorf("env file missing %q:\n%s", want, env)
				}
			}

			for key, want := range map[string]string{
				"driver":       testCase.wantDriver,
				"storage-conf": got.StorageConf,
				"storage-root": got.StorageRoot,
			} {
				if sink.Single(key) != want {
					t.Errorf("output %s = %q, want %q", key, sink.Single(key), want)
				}
			}

			if !strings.Contains(summary.body, "* Driver: `"+testCase.wantDriver+"`") {
				t.Errorf("summary does not report the driver:\n%s", summary.body)
			}
		})
	}
}

func TestSetupBuildah_InstallsOnlyMissingPackages(t *testing.T) {
	t.Parallel()

	scratch := t.TempDir()
	tool := &fakeBuildahSetupTool{commands: map[string]bool{
		"apt-get": true,
		"buildah": true,
		"curl":    true,
	}}
	installer := &fakePackageInstaller{}

	_, err := appcontainer.SetupBuildah(context.Background(), tool, installer, fakeoutputsink.New(t), nil, io.Discard, appcontainer.SetupBuildahInput{
		ExtraPackages:   "curl gettext custom buildah",
		InstallPackages: true,
		ProbeBuild:      false,
		StorageConf:     filepath.Join(scratch, "storage.conf"),
		StorageRoot:     filepath.Join(scratch, "root"),
		TmpDir:          filepath.Join(scratch, "tmp"),
		RunnerTemp:      scratch,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"fuse-overlayfs", "gettext", "custom"}
	if !reflect.DeepEqual(installer.packages, want) {
		t.Fatalf("installed packages = %v, want %v", installer.packages, want)
	}
}

func TestSetupBuildah_RejectsUnsafeInputs(t *testing.T) {
	t.Parallel()

	tool := &fakeBuildahSetupTool{commands: map[string]bool{"buildah": true}}

	if _, err := appcontainer.SetupBuildah(context.Background(), tool, nil, nil, nil, io.Discard, appcontainer.SetupBuildahInput{
		ExtraPackages:   "evil;rm",
		InstallPackages: false,
	}); err == nil || !strings.Contains(err.Error(), "unsafe apt package name") {
		t.Fatalf("unsafe package error = %v", err)
	}

	if _, err := appcontainer.SetupBuildah(context.Background(), tool, nil, nil, nil, io.Discard, appcontainer.SetupBuildahInput{
		InstallPackages: false,
		StorageRoot:     "/tmp",
	}); err == nil || !strings.Contains(err.Error(), "unsafe container storage root") {
		t.Fatalf("unsafe root error = %v", err)
	}
}

func storageDriverFromEnv(env []string) (string, error) {
	storageConf := ""

	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "CONTAINERS_STORAGE_CONF="); ok {
			storageConf = value
		}
	}

	if storageConf == "" {
		return "", errors.New("missing CONTAINERS_STORAGE_CONF") //nolint:err113 // test double error.
	}

	body, err := os.ReadFile(storageConf) //nolint:gosec // test-controlled path.
	if err != nil {
		return "", err
	}

	if strings.Contains(string(body), `driver = "overlay"`) {
		return "overlay", nil
	}

	return "vfs", nil
}

func readText(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path.
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

// TestSetupBuildah_ProbeWritesIntoADirectoryItsBaseOwns pins the one property
// that makes the probe worth running.
//
// It used to build "FROM scratch, COPY into /", which is the single build that
// cannot fail the way real builds fail: a scratch image owns nothing, so no
// directory is created inside an existing layer and nothing is copied up. On a
// nested runner whose container storage sits on its own overlay filesystem the
// probe passed and every real build failed, and the report arrived an hour into
// a release as "mkdir /usr/local: operation not permitted" -- naming neither
// the driver nor the nesting.
//
// So this asserts the shape rather than the bytes: a second stage built on the
// first, writing into a directory that first stage already owns. Both stages
// stay offline, which is why the base is a stage and not a pulled image.
func TestSetupBuildah_ProbeWritesIntoADirectoryItsBaseOwns(t *testing.T) {
	t.Parallel()

	tool := &fakeBuildahSetupTool{commands: map[string]bool{
		"buildah": true, "fuse-overlayfs": true, "apt-get": true,
	}}
	runnerTemp := t.TempDir()

	if _, err := appcontainer.SetupBuildah(
		context.Background(), tool, &fakePackageInstaller{}, nil, nil, io.Discard,
		appcontainer.SetupBuildahInput{ProbeBuild: true, RunnerTemp: runnerTemp, EnvFile: filepath.Join(runnerTemp, "env")},
	); err != nil {
		t.Fatalf("SetupBuildah: %v", err)
	}

	body := tool.probeContainerfile
	if body == "" {
		t.Fatal("the probe wrote no Containerfile")
	}

	if strings.Count(body, "FROM ") < 2 {
		t.Fatalf("the probe builds a single stage, so nothing it copies lands in a layer it does not own:\n%s", body)
	}

	if !strings.Contains(body, "FROM scratch AS owner") || !strings.Contains(body, "FROM owner") {
		t.Fatalf("the probe's second stage is not built on its first, so no copy-up is exercised:\n%s", body)
	}

	if strings.Count(body, "COPY probe.txt /owned/") != 2 {
		t.Fatalf("the probe does not write twice into the directory its base owns:\n%s", body)
	}
}

// TestSetupBuildah_ProbeCopiesFilesThatExist checks the probe build could
// actually run. Its sibling above pins the Containerfile's shape, which is
// the closest a unit test gets to "this build exercises copy-up" -- but a
// Containerfile naming a COPY source that was never written fails every
// real probe while satisfying any assertion made about its text.
//
// So this reads the sources out of the Containerfile the product wrote and
// requires each to be a real file in the probe directory. It survives
// renaming probe.txt or adding a stage; it fails if the two halves of
// runBuildahProbe stop agreeing.
func TestSetupBuildah_ProbeCopiesFilesThatExist(t *testing.T) {
	t.Parallel()

	tool := &fakeBuildahSetupTool{commands: map[string]bool{
		"buildah": true, "fuse-overlayfs": true, "apt-get": true,
	}}
	runnerTemp := t.TempDir()

	if _, err := appcontainer.SetupBuildah(
		context.Background(), tool, &fakePackageInstaller{}, nil, nil, io.Discard,
		appcontainer.SetupBuildahInput{ProbeBuild: true, RunnerTemp: runnerTemp, EnvFile: filepath.Join(runnerTemp, "env")},
	); err != nil {
		t.Fatalf("SetupBuildah: %v", err)
	}

	sources := copySources(t, tool.probeContainerfile)
	if len(sources) == 0 {
		t.Fatalf("the probe Containerfile copies nothing:\n%s", tool.probeContainerfile)
	}

	for _, source := range sources {
		if info, err := os.Stat(filepath.Join(tool.probeDir, source)); err != nil || !info.Mode().IsRegular() {
			t.Errorf("COPY %s names a file the probe never wrote into %s: %v", source, tool.probeDir, err)
		}
	}
}

// copySources returns the source operand of every COPY instruction.
func copySources(t *testing.T, containerfile string) []string {
	t.Helper()

	var sources []string

	for _, line := range strings.Split(containerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.EqualFold(fields[0], "COPY") {
			sources = append(sources, fields[1])
		}
	}

	return sources
}

// TestSetupBuildah_ProbeRemovesAStaleImageBeforeBuilding covers the reason
// the probe deletes before it builds: the probe image name is fixed, so on
// a self-hosted runner an image left by an earlier run is already there.
// Building over it without removing it first would let a probe "succeed"
// against storage that cannot in fact build anything.
func TestSetupBuildah_ProbeRemovesAStaleImageBeforeBuilding(t *testing.T) {
	t.Parallel()

	tool := &fakeBuildahSetupTool{commands: map[string]bool{
		"buildah": true, "fuse-overlayfs": true, "apt-get": true,
	}}
	runnerTemp := t.TempDir()

	if _, err := appcontainer.SetupBuildah(
		context.Background(), tool, &fakePackageInstaller{}, nil, nil, io.Discard,
		appcontainer.SetupBuildahInput{ProbeBuild: true, RunnerTemp: runnerTemp, EnvFile: filepath.Join(runnerTemp, "env")},
	); err != nil {
		t.Fatalf("SetupBuildah: %v", err)
	}

	if len(tool.calls) < 2 {
		t.Fatalf("calls = %v, want a removal followed by a build", tool.calls)
	}

	if !strings.HasPrefix(tool.calls[0], "remove:") || tool.calls[1] != "build" {
		t.Fatalf("calls = %v, want the stale image removed before the build", tool.calls)
	}

	// The removal has to name the image the build then produces, or it
	// deletes something else and the stale one survives.
	removed := strings.TrimPrefix(tool.calls[0], "remove:")
	if removed == "" {
		t.Error("the probe removed an image with no name")
	}
}
