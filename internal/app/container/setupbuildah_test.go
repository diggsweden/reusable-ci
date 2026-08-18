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
	commands         map[string]bool
	failOverlayProbe bool
	probeCalls       []string
	infoJSON         []byte
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

func (f *fakeBuildahSetupTool) ProbeBuild(_ context.Context, env []string, _, _ string, _ io.Writer) error {
	driver, err := storageDriverFromEnv(env)
	if err != nil {
		return err
	}

	f.probeCalls = append(f.probeCalls, driver)
	if driver == "overlay" && f.failOverlayProbe {
		return errors.New("simulated overlay probe failure") //nolint:err113 // test double error.
	}

	return nil
}

func (f *fakeBuildahSetupTool) RemoveImage(_ context.Context, _ []string, _ string, _ io.Writer) error {
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
