// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package jsonsink_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/jsonsink"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestSink_EmitsKeysInInsertionOrder covers the ordering the package
// documents and callers rely on: GoMetadata builds its outputs as a slice
// of pairs rather than a map precisely so this order is stable.
func TestSink_EmitsKeysInInsertionOrder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := jsonsink.New(&buf)
	ctx := context.Background()

	// Deliberately not alphabetical: encoding/json would sort these, and
	// the package exists partly to not do that.
	for _, k := range []string{"version", "binary-name", "module"} {
		if err := s.Set(ctx, k, "v-"+k); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	want := "{\n" +
		"  \"version\": \"v-version\",\n" +
		"  \"binary-name\": \"v-binary-name\",\n" +
		"  \"module\": \"v-module\"\n" +
		"}\n"
	if got := buf.String(); got != want {
		t.Errorf("document =\n%s\nwant\n%s", got, want)
	}
}

// TestSink_OutputIsValidJSON round-trips the document. The writer is
// hand-rolled rather than an encoding/json encoder, so the separators,
// indentation and escaping are this package's responsibility.
func TestSink_OutputIsValidJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := jsonsink.New(&buf)
	ctx := context.Background()

	// Values and a key that would break a naive concatenating writer.
	if err := s.Set(ctx, "quote\"key", `he said "hi"`); err != nil {
		t.Fatal(err)
	}

	if err := s.Set(ctx, "newlines", "one\ntwo\r\nthree"); err != nil {
		t.Fatal(err)
	}

	if err := s.Set(ctx, "backslash", `C:\path\to`); err != nil {
		t.Fatal(err)
	}

	if err := s.Set(ctx, "unicode", "räksmörgås 🐟"); err != nil {
		t.Fatal(err)
	}

	if err := s.SetBool(ctx, "is-snapshot", true); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, buf.String())
	}

	want := map[string]any{
		"quote\"key":  `he said "hi"`,
		"newlines":    "one\ntwo\r\nthree",
		"backslash":   `C:\path\to`,
		"unicode":     "räksmörgås 🐟",
		"is-snapshot": true,
	}
	for k, wantV := range want {
		if got[k] != wantV {
			t.Errorf("%q = %#v, want %#v", k, got[k], wantV)
		}
	}
}

// TestSink_BoolIsNativeJSON pins the reason SetBool exists: the package
// comment says consumers should be able to write `select(.is_snapshot)`
// in jq rather than comparing against the string "true".
func TestSink_BoolIsNativeJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := jsonsink.New(&buf)
	ctx := context.Background()

	if err := s.SetBool(ctx, "is-snapshot", false); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); !strings.Contains(got, `"is-snapshot": false`) {
		t.Errorf("document = %s, want an unquoted false", got)
	}
}

// TestSink_MultilineJoinsWithNewlines pins the documented equivalence:
// JSON has no heredoc, so a multi-line value is the joined string Set
// would have produced.
func TestSink_MultilineJoinsWithNewlines(t *testing.T) {
	t.Parallel()

	var multi, scalar bytes.Buffer

	ctx := context.Background()

	m := jsonsink.New(&multi)
	if err := m.SetMultiline(ctx, "tags", []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}

	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}

	sc := jsonsink.New(&scalar)
	if err := sc.Set(ctx, "tags", strings.Join([]string{"a", "b", "c"}, "\n")); err != nil {
		t.Fatal(err)
	}

	if err := sc.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if multi.String() != scalar.String() {
		t.Errorf("SetMultiline = %s, want the same document as the joined Set\n%s", multi.String(), scalar.String())
	}
}

// TestSink_RewriteKeepsPosition covers the documented overwrite rule: a
// repeated key takes the new value at its original place, so a caller
// that corrects an output does not reorder the document.
func TestSink_RewriteKeepsPosition(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := jsonsink.New(&buf)
	ctx := context.Background()

	for _, kv := range [][2]string{{"first", "1"}, {"second", "2"}, {"first", "rewritten"}} {
		if err := s.Set(ctx, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	want := "{\n  \"first\": \"rewritten\",\n  \"second\": \"2\"\n}\n"
	if got := buf.String(); got != want {
		t.Errorf("document =\n%s\nwant\n%s", got, want)
	}
}

func TestSink_RejectsEmptyKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := jsonsink.New(&bytes.Buffer{})

	if err := s.Set(ctx, "", "v"); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("Set err = %v, want ErrValidation", err)
	}

	if err := s.SetBool(ctx, "", true); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("SetBool err = %v, want ErrValidation", err)
	}

	if err := s.SetMultiline(ctx, "", []string{"v"}); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("SetMultiline err = %v, want ErrValidation", err)
	}
}

// TestSink_CloseSemantics covers the three claims Close makes: nothing is
// written when no key was set, it is idempotent, and every write path
// fails afterwards so misuse is loud rather than silently dropped.
func TestSink_CloseSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("no keys writes nothing", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		s := jsonsink.New(&buf)
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}

		// Not "{}": a command that set no outputs emits nothing at all,
		// so a caller piping into jq sees an empty stream rather than an
		// empty object.
		if buf.Len() != 0 {
			t.Errorf("wrote %q for a sink with no keys", buf.String())
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		s := jsonsink.New(&buf)
		if err := s.Set(ctx, "k", "v"); err != nil {
			t.Fatal(err)
		}

		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}

		first := buf.String()

		if err := s.Close(ctx); err != nil {
			t.Errorf("second Close = %v, want nil", err)
		}

		if buf.String() != first {
			t.Errorf("second Close wrote again: %q", buf.String())
		}
	})

	t.Run("writes after close fail", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		s := jsonsink.New(&buf)
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}

		if err := s.Set(ctx, "k", "v"); err == nil {
			t.Error("Set after Close did not error")
		}

		if err := s.SetBool(ctx, "k", true); err == nil {
			t.Error("SetBool after Close did not error")
		}

		if err := s.SetMultiline(ctx, "k", []string{"v"}); err == nil {
			t.Error("SetMultiline after Close did not error")
		}

		if buf.Len() != 0 {
			t.Errorf("a rejected write reached the document: %q", buf.String())
		}
	})
}
