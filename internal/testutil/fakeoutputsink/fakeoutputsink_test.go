// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakeoutputsink_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestSink_SetAndRead(t *testing.T) {
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
	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	want := []string{"a", "b", "c"}
	if err := s.SetMultiline(context.Background(), "tags", want); err != nil {
		t.Fatal(err)
	}

	got := s.Multiline("tags")
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Multiline(tags) = %v, want %v", got, want)
	}
}

func TestSink_RejectsAfterClose(t *testing.T) {
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
	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ctx := context.Background()
	_ = s.Set(ctx, "z", "1")
	_ = s.Set(ctx, "a", "2")
	_ = s.SetMultiline(ctx, "m", []string{"x"})

	keys := s.Keys()
	if strings.Join(keys, ",") != "a,m,z" {
		t.Errorf("Keys() = %v, want [a m z]", keys)
	}
}

func TestSink_AllScalar_IsCopy(t *testing.T) {
	s := fakeoutputsink.New(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_ = s.Set(context.Background(), "k", "v")

	all := s.AllScalar()
	all["k"] = "tampered"

	if got := s.Single("k"); got != "v" {
		t.Errorf("AllScalar mutation leaked into sink: got %q", got)
	}
}
