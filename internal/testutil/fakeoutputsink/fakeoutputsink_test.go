// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakeoutputsink_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestSink_SetAndRead(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ctx := context.Background()

	if err := s.Set(ctx, "version", "1.2.3"); err != nil {
		t.Fatal(err)
	}

	if got := s.Single("version"); got != "1.2.3" {
		t.Errorf("Single(version) = %q, want %q", got, "1.2.3")
	}

	if got := s.Single("missing"); got != "" {
		t.Errorf("Single(missing) = %q, want empty", got)
	}
}

func TestSink_Multiline(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	want := []string{"a", "b", "c"}
	if err := s.SetMultiline(context.Background(), "tags", want); err != nil {
		t.Fatal(err)
	}

	if got := s.Multiline("tags"); !slices.Equal(got, want) {
		t.Errorf("Multiline(tags) = %v, want %v", got, want)
	}
}

func TestSink_MultilineOwnershipAndAbsence(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	require.Nil(t, sink.Multiline("missing"))

	for _, lines := range [][]string{nil, {}} {
		require.NoError(t, sink.SetMultiline(t.Context(), "empty", lines))
		got := sink.Multiline("empty")
		require.NotNil(t, got)
		require.Empty(t, got)
	}

	lines := []string{"first", "second"}
	require.NoError(t, sink.SetMultiline(t.Context(), "tags", lines))
	lines[0] = "caller mutation"
	snapshot := sink.Multiline("tags")
	require.Equal(t, []string{"first", "second"}, snapshot)
	snapshot[1] = "snapshot mutation"

	require.Equal(t, []string{"first", "second"}, sink.Multiline("tags"))
	require.Equal(t, []string{"empty", "tags"}, sink.Keys())
	require.Nil(t, sink.Multiline("missing"))
}

func TestSink_RejectsAfterClose(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ctx := context.Background()

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if err := s.Set(ctx, "k", "v"); err == nil {
		t.Errorf("Set after Close did not return error")
	}

	if err := s.SetMultiline(ctx, "k", []string{"v"}); err == nil {
		t.Errorf("SetMultiline after Close did not return error")
	}

	if got := s.CloseCount(); got != 1 {
		t.Errorf("CloseCount = %d, want 1", got)
	}
}

func TestSink_KeysSorted(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ctx := context.Background()
	_ = s.Set(ctx, "z", "1")
	_ = s.Set(ctx, "a", "2")
	_ = s.SetMultiline(ctx, "m", []string{"x"})

	want := []string{"a", "m", "z"}
	if got := s.Keys(); !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
}

func TestSink_AllScalar_IsCopy(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_ = s.Set(context.Background(), "k", "v")

	all := s.AllScalar()
	all["k"] = "tampered"

	if got := s.Single("k"); got != "v" {
		t.Errorf("AllScalar mutation leaked into sink: got %q", got)
	}
}

func TestSink_OrderIsInsertionOrder(t *testing.T) {
	t.Parallel()

	s := fakeoutputsink.New(t)
	ctx := context.Background()

	// Deliberately not alphabetical, so a sorted result would not pass.
	for _, k := range []string{"zebra", "alpha", "middle"} {
		if err := s.Set(ctx, k, "v"); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SetMultiline(ctx, "lines", []string{"a"}); err != nil {
		t.Fatal(err)
	}

	// A rewrite keeps the original position, matching jsonsink.
	if err := s.Set(ctx, "zebra", "v2"); err != nil {
		t.Fatal(err)
	}

	want := []string{"zebra", "alpha", "middle", "lines"}
	if got := s.Order(); !slices.Equal(got, want) {
		t.Errorf("Order() = %q, want %q", got, want)
	}
}

// TestSink_SetBoolFormatsLikeTheStringSinks pins the one conversion this fake
// performs.
//
// SetBool exists so a test asserting on scalar output does not have to know
// which format the production sink uses, and the GHA and GitLab sinks write
// "true"/"false". If it drifted — "1"/"0", or Go's %v on some other type — every
// test reading a boolean output through this fake would agree with the fake and
// disagree with the runner.
func TestSink_SetBoolFormatsLikeTheStringSinks(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	if err := sink.SetBool(context.Background(), "yes", true); err != nil {
		t.Fatal(err)
	}

	if err := sink.SetBool(context.Background(), "no", false); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("yes"); got != "true" {
		t.Errorf("SetBool(true) stored %q, want %q", got, "true")
	}

	if got := sink.Single("no"); got != "false" {
		t.Errorf("SetBool(false) stored %q, want %q", got, "false")
	}
}

// TestSink_RefusesANewlineWithoutChangingState covers the guard that keeps a
// multi-line value from being written as a scalar.
//
// The real GHA sink writes `key=value` on one line, so a scalar containing a
// newline would inject an additional, attacker- or accident-controlled output
// line. The fake refuses instead, and what matters as much as the refusal is
// that it changes nothing: a test asserting "the refused key was not published"
// needs the sink to still be empty afterwards.
func TestSink_RefusesANewlineWithoutChangingState(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	if err := sink.Set(context.Background(), "kept", "fine"); err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{"a\nb", "a\rb", "trailing\n"} {
		if err := sink.Set(context.Background(), "injected", value); err == nil {
			t.Errorf("Set accepted a scalar containing a newline: %q", value)
		}
	}

	if got := sink.Keys(); len(got) != 1 || got[0] != "kept" {
		t.Errorf("keys = %v, want only [kept]: a refused write left state behind", got)
	}

	if got := sink.Single("injected"); got != "" {
		t.Errorf("the refused key holds %q", got)
	}
}

// TestSink_ScalarAndMultilineAreSeparateNamespaces states what the fake does
// when the same key is written both ways, because it is not obvious and callers
// have to know which reader to use.
//
// The two live in separate maps, so writing both leaves both readable and Keys
// reports the key once. This documents the behaviour rather than changing it:
// no caller writes a key both ways, and a fake that silently dropped one would
// be the more surprising of the two.
func TestSink_ScalarAndMultilineAreSeparateNamespaces(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)

	if err := sink.Set(context.Background(), "dual", "scalar"); err != nil {
		t.Fatal(err)
	}

	if err := sink.SetMultiline(context.Background(), "dual", []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("dual"); got != "scalar" {
		t.Errorf("Single = %q, want %q", got, "scalar")
	}

	if got := sink.Multiline("dual"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("Multiline = %v, want [one two]", got)
	}

	if got := sink.Keys(); !slices.Equal(got, []string{"dual"}) {
		t.Errorf("Keys = %v, want the key reported once", got)
	}
}

// TestSink_CloseCountIsOptIn pins the behaviour the constructor's doc used to
// describe as the opposite: nothing is asserted automatically, so a sink that
// is never closed passes, and one closed twice is visible only to a test that
// looks.
func TestSink_CloseCountIsOptIn(t *testing.T) {
	t.Parallel()

	sink := fakeoutputsink.New(t)
	if got := sink.CloseCount(); got != 0 {
		t.Errorf("CloseCount on a fresh sink = %d, want 0", got)
	}

	for range 2 {
		if err := sink.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if got := sink.CloseCount(); got != 2 {
		t.Errorf("CloseCount = %d, want 2; a double close must be observable", got)
	}
}
