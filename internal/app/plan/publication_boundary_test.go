// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

var errPublicationBoundary = errors.New("owned output failure")

type patternSink struct {
	*fakeoutputsink.Sink
	events *[]string
	fail   bool
}

func (s *patternSink) Set(ctx context.Context, key, value string) error {
	*s.events = append(*s.events, "sink")
	if s.fail {
		return errPublicationBoundary
	}

	return s.Sink.Set(ctx, key, value)
}

type patternWriter struct {
	bytes.Buffer
	events *[]string
	fail   bool
}

func (w *patternWriter) Write(body []byte) (int, error) {
	*w.events = append(*w.events, "writer")
	if w.fail {
		return 0, errPublicationBoundary
	}

	return w.Buffer.Write(body)
}

func TestPatternPublicationBoundary_PreflightAndCommitOrder(t *testing.T) { //nolint:gocognit // each output mode is tested at both failure boundaries and with a successful control.
	t.Parallel()

	for _, format := range []output.Format{output.FormatText, output.FormatJSON, output.FormatGitHub, output.FormatGitLab} {
		for _, failure := range []string{"none", "writer", "sink"} {
			var events []string

			sink := &patternSink{Sink: fakeoutputsink.New(t), events: &events, fail: failure == "sink"}
			writer := &patternWriter{events: &events, fail: failure == "writer"}
			_, err := appplan.GetFilePattern(t.Context(), sink, writer, appplan.GetFilePatternInput{CustomPattern: "CHANGELOG.md", Format: format, WriteToOutput: true})
			onlySink := format == output.FormatGitHub || format == output.FormatGitLab

			wantEvents := []string{"writer", "sink"}
			if onlySink {
				wantEvents = []string{"sink"}
			} else if failure == "writer" {
				wantEvents = []string{"writer"}
			}

			if !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("format=%s failure=%s events=%v", format, failure, events)
			}

			wantError := failure == "sink" || (failure == "writer" && !onlySink)
			if wantError && !errors.Is(err, errPublicationBoundary) || !wantError && err != nil {
				t.Fatalf("format=%s failure=%s err=%v", format, failure, err)
			}

			if !onlySink && failure != "writer" {
				want := "CHANGELOG.md\n"
				if format == output.FormatJSON {
					want = "{\"pattern\":\"CHANGELOG.md\"}\n"
				}

				if writer.String() != want {
					t.Fatalf("bytes=%q", writer.String())
				}
			}
		}
	}

	for _, in := range []appplan.GetFilePatternInput{{CustomPattern: "x", Format: "invalid"}, {CustomPattern: "x", WriteToOutput: true}, {CustomPattern: "x", Format: output.FormatGitHub}} {
		var out bytes.Buffer
		if _, err := appplan.GetFilePattern(t.Context(), nil, &out, in); err == nil || out.Len() != 0 {
			t.Fatalf("preflight err=%v out=%s", err, &out)
		}
	}
}
