// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeTrivy implements appsecurity.TrivyOps, recording invocations and writing a
// zero-findings JSON to the --output path so ScanContainer treats the scan as
// clean (no SARIF/report, no gate failure).
type fakeTrivy struct{ runs [][]string }

func (f *fakeTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.runs = append(f.runs, args)

	for i, a := range args {
		if a == "--output" && i+1 < len(args) {
			_ = os.WriteFile(args[i+1], []byte(`{"Results":[]}`), 0o644) //nolint:gosec // test fixture
		}
	}

	return 0, nil
}

func pushByDigestInput() appcontainer.BuildImageInput {
	return appcontainer.BuildImageInput{
		Context:       ".",
		Containerfile: "Containerfile",
		Platform:      "linux/amd64",
		Mode:          container.BuildModePushByDigest,
		ImageRef:      "ghcr.io/org/app",
	}
}

func TestBuildAndScan_BuildsThenScansThreadingDigest(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate the scan's default output files

	builder := &fakeBuilder{}
	pusher := &fakePusher{digest: "sha256:abc"}
	scanner := &fakeTrivy{}
	sink := fakeoutputsink.New(t)

	digest, err := appcontainer.BuildAndScan(context.Background(), builder, pusher, scanner, sink, io.Discard, io.Discard, output.Annotator{}, appcontainer.BuildAndScanInput{
		Build:        pushByDigestInput(),
		EnableScan:   true,
		ScanSeverity: "CRITICAL,HIGH",
	})
	if err != nil {
		t.Fatal(err)
	}

	if digest != "sha256:abc" {
		t.Errorf("digest = %q", digest)
	}

	if sink.Single("digest") != "sha256:abc" {
		t.Errorf("digest not emitted on sink: %q", sink.Single("digest"))
	}

	// The scan ran, anchored to name@digest.
	if len(scanner.runs) == 0 {
		t.Fatal("trivy scan did not run")
	}

	if !strings.Contains(flatten(scanner.runs), "ghcr.io/org/app@sha256:abc") {
		t.Errorf("scan not anchored to name@digest: %v", scanner.runs)
	}
}

func TestBuildAndScan_NoScanWhenDisabled(t *testing.T) {
	t.Parallel()

	scanner := &fakeTrivy{}

	_, err := appcontainer.BuildAndScan(context.Background(), &fakeBuilder{}, &fakePusher{digest: "sha256:abc"}, scanner, fakeoutputsink.New(t), io.Discard, io.Discard, output.Annotator{}, appcontainer.BuildAndScanInput{
		Build:      pushByDigestInput(),
		EnableScan: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(scanner.runs) != 0 {
		t.Errorf("scan ran despite EnableScan=false: %v", scanner.runs)
	}
}

func TestBuildAndScan_NoScanWhenNoDigest(t *testing.T) {
	t.Parallel()

	scanner := &fakeTrivy{}

	// load mode emits no digest → nothing to scan.
	_, err := appcontainer.BuildAndScan(context.Background(), &fakeBuilder{}, &fakePusher{}, scanner, fakeoutputsink.New(t), io.Discard, io.Discard, output.Annotator{}, appcontainer.BuildAndScanInput{
		Build:      appcontainer.BuildImageInput{Context: ".", Containerfile: "Containerfile", Mode: container.BuildModeLoad, ImageRef: "app:test"},
		EnableScan: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(scanner.runs) != 0 {
		t.Errorf("scan ran despite no digest (load mode): %v", scanner.runs)
	}
}

func flatten(runs [][]string) string {
	parts := make([]string, 0, len(runs))
	for _, r := range runs {
		parts = append(parts, strings.Join(r, " "))
	}

	return strings.Join(parts, " ")
}
