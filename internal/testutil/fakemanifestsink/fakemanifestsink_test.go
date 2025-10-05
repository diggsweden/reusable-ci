// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakemanifestsink_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
)

type manifestBody struct {
	Stage string `json:"stage"`
}

func (m manifestBody) MarshalJSON() ([]byte, error) {
	type alias manifestBody

	return json.Marshal(alias(m))
}

func TestSink_RecordsWritesByStage(t *testing.T) {
	t.Parallel()

	sink := fakemanifestsink.New(t)
	if err := sink.Write(context.Background(), "build", map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}

	if err := sink.WriteJSON(context.Background(), "publish", manifestBody{Stage: "publish"}); err != nil {
		t.Fatal(err)
	}

	if err := sink.Write(context.Background(), "build", map[string]any{"ok": false}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Body("build"); got != `{"ok":false}` {
		t.Errorf("build body = %s", got)
	}

	if got := sink.Body("publish"); got != `{"stage":"publish"}` {
		t.Errorf("publish body = %s", got)
	}

	if got := sink.Body("missing"); got != "" {
		t.Errorf("missing body = %q", got)
	}

	if got, want := sink.Stages(), []string{"build", "publish"}; !slices.Equal(got, want) {
		t.Errorf("stages = %v, want %v", got, want)
	}
}

// errMarshal stands in for a result the real sink could not encode.
var errMarshal = errors.New("cannot marshal") //nolint:err113 // test fixture sentinel.

type failingBody struct{}

func (failingBody) MarshalJSON() ([]byte, error) { return nil, errMarshal }

// TestSink_OrderCopiesAndFailures covers what the recording test above cannot
// see.
//
// Its stages happen to be alphabetical, so a fake that sorted them passed; here
// they are written in reverse alphabetical order. The returned Stages slice is
// the caller's to modify, and a caller that does must not rewrite the sink's
// record. And a write that fails to encode must fail and record nothing: the
// real sink never persists an unencodable result, so a fake that did would let
// an app test pass for code that ignores the error.
func TestSink_OrderCopiesAndFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sink := fakemanifestsink.New(t)

	for _, stage := range []string{"publish", "build", "publish"} {
		if err := sink.Write(ctx, stage, map[string]any{"stage": stage}); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := sink.Stages(), []string{"publish", "build"}; !slices.Equal(got, want) {
		t.Errorf("stages = %v, want first-write order %v", got, want)
	}

	detached := sink.Stages()
	detached[0] = "tampered"

	if got := sink.Stages()[0]; got != "publish" {
		t.Errorf("modifying the returned slice changed the sink: first stage = %q", got)
	}

	// A map that encoding/json refuses, and a marshaler that fails.
	if err := sink.Write(ctx, "build", map[string]any{"bad": func() {}}); err == nil {
		t.Error("an unencodable map was accepted")
	}

	if err := sink.WriteJSON(ctx, "sbom", failingBody{}); !errors.Is(err, errMarshal) {
		t.Errorf("WriteJSON err = %v, want the marshaler's own cause", err)
	}

	if got := sink.Body("build"); got != `{"stage":"build"}` {
		t.Errorf("a failed write replaced the recorded body: %s", got)
	}

	if got, want := sink.Stages(), []string{"publish", "build"}; !slices.Equal(got, want) {
		t.Errorf("a failed write changed the stages: %v, want %v", got, want)
	}
}
