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
	pusher := &fakePusher{digest: oneDigest}
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

	if digest != oneDigest {
		t.Errorf("digest = %q", digest)
	}

	if sink.Single("digest") != oneDigest {
		t.Errorf("digest not emitted on sink: %q", sink.Single("digest"))
	}

	// The scan ran, anchored to name@digest.
	if len(scanner.runs) == 0 {
		t.Fatal("trivy scan did not run")
	}

	if !strings.Contains(flatten(scanner.runs), "ghcr.io/org/app@"+oneDigest) {
		t.Errorf("scan not anchored to name@digest: %v", scanner.runs)
	}
}

func TestBuildAndScan_NoScanWhenDisabled(t *testing.T) {
	t.Parallel()

	scanner := &fakeTrivy{}

	_, err := appcontainer.BuildAndScan(context.Background(), &fakeBuilder{}, &fakePusher{digest: oneDigest}, scanner, fakeoutputsink.New(t), io.Discard, io.Discard, output.Annotator{}, appcontainer.BuildAndScanInput{
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

// The severity a caller configures is both the trivy `--severity` filter and
// the fail-on threshold, so it decides which findings are reported AND which
// ones fail the build. `containers[].scan-severity` in an adopter's
// artifacts.yml reaches trivy through here.
//
// What the tests above check is that a scan ran and was anchored to
// name@digest. Neither looks at the severity, so the value could arrive
// truncated, defaulted, or dropped from the argv entirely and every one of them
// would pass — while the gate silently widened to report everything or narrowed
// to report nothing.
func TestBuildAndScan_ForwardsTheExactRequestedSeverity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested string
		want      string
		why       string
	}{
		{"the default gate", "CRITICAL,HIGH", "CRITICAL,HIGH", "what an adopter gets without configuring anything"},
		{"a narrowed gate", "CRITICAL", "CRITICAL", "relaxing the gate is a deliberate per-container choice"},
		{"a widened gate", "CRITICAL,HIGH,MEDIUM,LOW", "CRITICAL,HIGH,MEDIUM,LOW", "every level survives, in order"},
		{"unset falls back to the documented default", "", "CRITICAL,HIGH", "an empty value must not become an empty filter, which trivy reads as no filter"},
		{"lowercase is normalised, not rejected", "critical,high", "CRITICAL,HIGH", "trivy matches these case-sensitively"},
		{"surrounding spaces are trimmed", "CRITICAL , HIGH", "CRITICAL,HIGH", "a comma-list written by a human"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			scanner := &fakeTrivy{}

			if _, err := appcontainer.BuildAndScan(context.Background(),
				&fakeBuilder{}, &fakePusher{digest: oneDigest}, scanner, fakeoutputsink.New(t),
				io.Discard, io.Discard, output.Annotator{},
				appcontainer.BuildAndScanInput{Build: pushByDigestInput(), EnableScan: true, ScanSeverity: tc.requested},
			); err != nil {
				t.Fatal(err)
			}

			got, ok := argValue(scanner.runs, "--severity")
			if !ok {
				t.Fatalf("no --severity in the trivy argv: %v; the gate would apply to every level", scanner.runs)
			}

			if got != tc.want {
				t.Errorf("--severity %q, want %q: %s", got, tc.want, tc.why)
			}
		})
	}
}

// An unsupported level must refuse rather than reach trivy, which would either
// error opaquely or, worse, ignore the unknown entry and scan with the rest.
func TestBuildAndScan_RefusesAnUnsupportedSeverity(t *testing.T) {
	t.Chdir(t.TempDir())

	scanner := &fakeTrivy{}

	_, err := appcontainer.BuildAndScan(context.Background(),
		&fakeBuilder{}, &fakePusher{digest: oneDigest}, scanner, fakeoutputsink.New(t),
		io.Discard, io.Discard, output.Annotator{},
		appcontainer.BuildAndScanInput{Build: pushByDigestInput(), EnableScan: true, ScanSeverity: "CRITICAL,SEVERE"},
	)
	if err == nil {
		t.Fatal("an unsupported severity reached the scan")
	}

	for _, run := range scanner.runs {
		if value, ok := argValue([][]string{run}, "--severity"); ok {
			t.Errorf("trivy ran with --severity %q despite the refusal", value)
		}
	}
}

// argValue returns the value following flag in the last recorded invocation
// that carries it.
func argValue(runs [][]string, flag string) (string, bool) {
	for i := len(runs) - 1; i >= 0; i-- {
		for j, arg := range runs[i] {
			if arg == flag && j+1 < len(runs[i]) {
				return runs[i][j+1], true
			}
		}
	}

	return "", false
}
