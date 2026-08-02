// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeRawContainerScanTrivy struct {
	args      [][]string
	code      int
	err       error
	failCount int
	body      string
}

func (f *fakeRawContainerScanTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.args = append(f.args, append([]string{}, args...))
	if f.failCount > 0 {
		f.failCount--

		return 1, nil
	}

	for i, arg := range args {
		if arg == "--output" && i+1 < len(args) {
			body := f.body
			if body == "" {
				body = `{"Results":[]}`
			}

			if err := os.WriteFile(args[i+1], []byte(body), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
				return 1, err
			}
		}
	}

	return f.code, f.err
}

func TestRawContainerScan_RunsTrivyValidatesJSONAndWritesOutput(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	output := filepath.Join(work, "trivy.json")
	sink := fakeoutputsink.New(t)
	trivy := &fakeRawContainerScanTrivy{}

	if err := appsecurity.RawContainerScan(context.Background(), trivy, sink, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
		ImageRef:   "codeberg.org/example/app@sha256:deadbeef",
		Platform:   "linux/arm64",
		Output:     output,
		Timeout:    "7m",
		Attempts:   1,
		RetryDelay: time.Nanosecond,
		Scanners:   "vuln,secret",
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"image", "--timeout", "7m", "--platform", "linux/arm64", "--scanners", "vuln,secret", "--skip-version-check", "--format", "json", "--output", output, "codeberg.org/example/app@sha256:deadbeef"}
	if !reflect.DeepEqual(trivy.args[0], want) {
		t.Fatalf("trivy args = %#v, want %#v", trivy.args[0], want)
	}

	if got := sink.Single("result-path"); got != output {
		t.Fatalf("result-path = %q, want %q", got, output)
	}
}

func TestRawContainerScan_RetriesFailedTrivy(t *testing.T) {
	t.Parallel()

	trivy := &fakeRawContainerScanTrivy{failCount: 1}
	if err := appsecurity.RawContainerScan(context.Background(), trivy, nil, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
		ImageRef:   "image",
		Output:     filepath.Join(t.TempDir(), "trivy.json"),
		Attempts:   2,
		RetryDelay: time.Nanosecond,
	}); err != nil {
		t.Fatal(err)
	}

	if len(trivy.args) != 2 {
		t.Fatalf("trivy runs = %d, want 2", len(trivy.args))
	}
}

func TestRawContainerScan_RejectsInvalidTrivyShape(t *testing.T) {
	t.Parallel()

	err := appsecurity.RawContainerScan(context.Background(), &fakeRawContainerScanTrivy{body: `{"unexpected":true}`}, nil, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
		ImageRef: "image",
		Output:   filepath.Join(t.TempDir(), "trivy.json"),
		Attempts: 1,
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}
}

func TestRawContainerScan_RejectsInvalidInputBeforeTools(t *testing.T) {
	t.Parallel()

	trivy := &fakeRawContainerScanTrivy{}

	err := appsecurity.RawContainerScan(context.Background(), trivy, nil, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
		ImageRef: "bad\nref",
		Output:   filepath.Join(t.TempDir(), "trivy.json"),
	})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "single line") {
		t.Fatalf("err = %v, want single-line usage error", err)
	}

	if len(trivy.args) != 0 {
		t.Fatalf("trivy invoked before validation: %v", trivy.args)
	}
}
