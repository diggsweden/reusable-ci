// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

type fakeRawContainerScanTrivy struct {
	args      [][]string
	code      int
	err       error
	failCount int
	body      string
	// noWrite exits with code and err without writing a report.
	noWrite bool
	// cancel runs after each call, so a test can cancel between attempts.
	cancel func()
}

func (f *fakeRawContainerScanTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.args = append(f.args, append([]string{}, args...))
	if f.cancel != nil {
		defer f.cancel()
	}

	if f.noWrite {
		return f.code, f.err
	}

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
	if !slices.Equal(trivy.args[0], want) {
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

	// The retry re-runs the same scan. Counting the runs cannot tell that
	// from a second attempt that dropped a scanner or changed the output
	// path, which would report a different result under the same name.
	if !slices.Equal(trivy.args[0], trivy.args[1]) {
		t.Errorf("retry ran different arguments:\n first=%q\nsecond=%q", trivy.args[0], trivy.args[1])
	}
}

func TestRawContainerScan_ValidatesReportBeforeSuccessOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"empty-object", `{}`, false},
		{"unknown-only", `{"unexpected":true}`, false},
		{"unnamed-null", `{"Results":null}`, false},
		{"blank-name", `{"ArtifactName":" \t"}`, false},
		{"typed-field", `{"Results":[{"Vulnerabilities":"invalid"}]}`, false},
		{"syntax", `{"Results":[`, false},
		{"empty-array", `{"Results":[],"Future":true}`, true},
		{"named-omitted", `{"ArtifactName":"image"}`, true},
		{"named-null", `{"ArtifactName":"image","Results":null}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "trivy.json")
			sink := fakeoutputsink.New(t)
			require.NoError(t, sink.Set(t.Context(), "prior", "keep"))

			trivy := &fakeRawContainerScanTrivy{body: tc.body}

			var out bytes.Buffer

			err := appsecurity.RawContainerScan(t.Context(), trivy, sink, &out, &out, appsecurity.RawContainerScanInput{ImageRef: "image", Output: path, Attempts: 1})
			require.Len(t, trivy.args, 1)

			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, map[string]string{"prior": "keep", "result-path": path}, sink.AllScalar())
				require.Contains(t, out.String(), "Trivy image scan succeeded")
			} else {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Equal(t, map[string]string{"prior": "keep"}, sink.AllScalar())
				require.NotContains(t, out.String(), "Trivy image scan succeeded")
			}

			if tc.name == "typed-field" {
				var cause *json.UnmarshalTypeError
				require.ErrorAs(t, err, &cause)
			}

			if tc.name == "syntax" {
				var cause *json.SyntaxError
				require.ErrorAs(t, err, &cause)
			}
			// Raw scans retain the scanner's file, even on validation failure.
			body, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, tc.body, string(body))
		})
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

// TestRawContainerScan_OnlyAFreshReportCounts covers the report file a
// previous run left behind. The scan deletes it before each attempt, so a
// Trivy that exits 0 without writing is a failed scan rather than a stale
// report published as this one -- and that failure is the tool's, not a bug
// in this binary. A report that cannot be deleted at all refuses before
// Trivy runs.
func TestRawContainerScan_OnlyAFreshReportCounts(t *testing.T) {
	t.Parallel()

	t.Run("exit 0 without a report", func(t *testing.T) {
		t.Parallel()

		output := filepath.Join(t.TempDir(), "trivy.json")
		require.NoError(t, os.WriteFile(output, []byte(`{"Results":[]}`), 0o600))

		sink := fakeoutputsink.New(t)
		trivy := &fakeRawContainerScanTrivy{noWrite: true}

		var out bytes.Buffer

		err := appsecurity.RawContainerScan(t.Context(), trivy, sink, &out, &out, appsecurity.RawContainerScanInput{ImageRef: "image", Output: output, Attempts: 1})
		require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
		require.Len(t, trivy.args, 1)
		require.Empty(t, sink.Keys())
		require.NotContains(t, out.String(), "succeeded")

		_, statErr := os.Stat(output)
		require.ErrorIs(t, statErr, os.ErrNotExist, "the stale report must not survive")
	})

	t.Run("a report that cannot be removed", func(t *testing.T) {
		t.Parallel()

		output := filepath.Join(t.TempDir(), "trivy.json")
		require.NoError(t, os.MkdirAll(filepath.Join(output, "occupied"), 0o750))

		sink := fakeoutputsink.New(t)
		trivy := &fakeRawContainerScanTrivy{}

		err := appsecurity.RawContainerScan(t.Context(), trivy, sink, io.Discard, io.Discard, appsecurity.RawContainerScanInput{ImageRef: "image", Output: output, Attempts: 3, RetryDelay: time.Nanosecond})
		require.ErrorIs(t, err, errs.ErrValidation)
		require.Empty(t, trivy.args, "trivy ran against an output that still held an old report")
		require.Empty(t, sink.Keys())
	})
}

// TestRawContainerScan_FailuresPublishNothing covers the ways a scan ends
// without a usable report: every attempt exits nonzero, the job is cancelled
// between attempts, or the output sink refuses the path. Each keeps its own
// cause, and none leaves a result-path behind.
func TestRawContainerScan_FailuresPublishNothing(t *testing.T) {
	t.Parallel()

	t.Run("attempts exhausted", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)
		trivy := &fakeRawContainerScanTrivy{failCount: 5}

		var stderr bytes.Buffer

		err := appsecurity.RawContainerScan(t.Context(), trivy, sink, io.Discard, &stderr, appsecurity.RawContainerScanInput{
			ImageRef: "image", Output: filepath.Join(t.TempDir(), "trivy.json"), Attempts: 2, RetryDelay: time.Nanosecond,
		})
		require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
		require.Contains(t, err.Error(), "exited with status 1")
		require.Len(t, trivy.args, 2)
		require.Empty(t, sink.Keys())
		require.Equal(t, "Trivy image scan failed (attempt 1/2) for linux/amd64, retrying...\n", stderr.String())
	})

	t.Run("cancelled between attempts", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		sink := fakeoutputsink.New(t)
		trivy := &fakeRawContainerScanTrivy{failCount: 5, cancel: cancel}

		err := appsecurity.RawContainerScan(ctx, trivy, sink, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
			ImageRef: "image", Output: filepath.Join(t.TempDir(), "trivy.json"), Attempts: 3, RetryDelay: time.Hour,
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, trivy.args, 1)
		require.Empty(t, sink.Keys())
	})

	t.Run("sink refuses the path", func(t *testing.T) {
		t.Parallel()

		errSinkClosed := errors.New("output file closed") //nolint:err113 // a unique value to find in the chain.
		sink := &failingSink{Sink: fakeoutputsink.New(t), failKey: "result-path", err: errSinkClosed}

		err := appsecurity.RawContainerScan(t.Context(), &fakeRawContainerScanTrivy{}, sink, io.Discard, io.Discard, appsecurity.RawContainerScanInput{
			ImageRef: "image", Output: filepath.Join(t.TempDir(), "trivy.json"), Attempts: 1,
		})
		require.ErrorIs(t, err, errSinkClosed)
		require.Empty(t, sink.Keys())
	})
}
