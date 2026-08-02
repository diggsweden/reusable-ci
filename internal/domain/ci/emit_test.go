// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// multilineSink records SetMultiline calls and can be told to reject them
// with a chosen error, standing in for GHA (supports) vs GitLab (returns
// ErrUnsupported).
type multilineSink struct {
	ci.OutputSink
	lines      map[string][]string
	rejectWith error
}

func newMultilineSink(reject error) *multilineSink {
	return &multilineSink{lines: map[string][]string{}, rejectWith: reject}
}

func (s *multilineSink) SetMultiline(_ context.Context, key string, lines []string) error {
	if s.rejectWith != nil {
		return fmt.Errorf("sink: %w", s.rejectWith)
	}

	s.lines[key] = lines

	return nil
}

// recordingManifest captures the single (stage, result) pair EmitMultiline
// falls back to.
type recordingManifest struct {
	ci.ManifestSink
	stage  string
	result map[string]any
	writes int
}

func (m *recordingManifest) Write(_ context.Context, stage string, result map[string]any) error {
	m.stage = stage
	m.result = result
	m.writes++

	return nil
}

func TestEmitMultiline_NativeSinkWritesLinesAndSkipsManifest(t *testing.T) {
	t.Parallel()

	sink := newMultilineSink(nil)
	manifest := &recordingManifest{}

	err := ci.EmitMultiline(context.Background(), sink, manifest, "container-metadata",
		ci.MultilineEntry{Key: "tags", Lines: []string{"a", "b"}},
		ci.MultilineEntry{Key: "labels", Lines: []string{"k=v"}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(sink.lines["tags"], []string{"a", "b"}) ||
		!reflect.DeepEqual(sink.lines["labels"], []string{"k=v"}) {
		t.Fatalf("sink lines = %#v", sink.lines)
	}

	if manifest.writes != 0 {
		t.Fatalf("manifest written %d times; a native sink must not touch it", manifest.writes)
	}
}

func TestEmitMultiline_UnsupportedSinkDegradesToManifestOnce(t *testing.T) {
	t.Parallel()

	// GitLab: the dotenv sink reports ErrUnsupported before writing, so every
	// entry must land in one stage manifest — not split across channels.
	sink := newMultilineSink(errs.ErrUnsupported)
	manifest := &recordingManifest{}

	err := ci.EmitMultiline(context.Background(), sink, manifest, "container-metadata",
		ci.MultilineEntry{Key: "tags", Lines: []string{"a", "b"}},
		ci.MultilineEntry{Key: "labels", Lines: []string{"k=v"}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(sink.lines) != 0 {
		t.Fatalf("unsupported sink recorded lines %#v; nothing should reach it", sink.lines)
	}

	if manifest.writes != 1 || manifest.stage != "container-metadata" {
		t.Fatalf("manifest writes=%d stage=%q, want one write to container-metadata", manifest.writes, manifest.stage)
	}

	want := map[string]any{"tags": []string{"a", "b"}, "labels": []string{"k=v"}}
	if !reflect.DeepEqual(manifest.result, want) {
		t.Fatalf("manifest result = %#v, want %#v", manifest.result, want)
	}
}

func TestEmitMultiline_UnsupportedSinkWithoutManifestErrors(t *testing.T) {
	t.Parallel()

	sink := newMultilineSink(errs.ErrUnsupported)

	err := ci.EmitMultiline(context.Background(), sink, nil, "changelog",
		ci.MultilineEntry{Key: "content", Lines: []string{"x"}})
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported when no manifest is available", err)
	}
}

func TestEmitMultiline_NonUnsupportedErrorPropagates(t *testing.T) {
	t.Parallel()

	sink := newMultilineSink(errs.ErrValidation)
	manifest := &recordingManifest{}

	err := ci.EmitMultiline(context.Background(), sink, manifest, "changelog",
		ci.MultilineEntry{Key: "content", Lines: []string{"x"}})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want the sink's validation error propagated", err)
	}

	if manifest.writes != 0 {
		t.Fatalf("manifest written on a non-unsupported error; must not fall back")
	}
}
