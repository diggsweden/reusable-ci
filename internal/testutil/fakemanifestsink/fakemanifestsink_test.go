// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakemanifestsink_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/fakemanifestsink"
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

	if got, want := sink.Stages(), []string{"build", "publish"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stages = %v, want %v", got, want)
	}
}
