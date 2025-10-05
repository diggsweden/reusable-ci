// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ghaoutput

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// errWriteFailed stands in for a full disk or a closed pipe.
var errWriteFailed = errors.New("write failed")

// recordingWriter counts Write calls and can be told to fail them, so the test
// can tell "wrote the block once" from "wrote it in pieces".
type recordingWriter struct {
	writes int
	fail   bool
	body   strings.Builder
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.fail {
		return 0, errWriteFailed
	}

	return w.body.Write(p)
}

func (w *recordingWriter) Close() error { return nil }

// TestSetMultiline_AFailedWriteCommitsNothing is the injected-failure control.
//
// This is an internal test on purpose. The failure is the underlying file
// refusing a write -- a full disk, a closed pipe -- and there is no way to make
// a real $GITHUB_OUTPUT do that on demand without depending on the host.
// Replacing the sink's writer is the smallest seam that reproduces it.
//
// $GITHUB_OUTPUT is append-only and shared by every step in the job, so an
// unterminated "key<<EOF_..." line is either a parse error the runner blames on
// the job or a heredoc that swallows every later output line as its content.
// Before the block was assembled up front, the opener was a committed write of
// its own and any later failure left exactly that.
func TestSetMultiline_AFailedWriteCommitsNothing(t *testing.T) {
	w := &recordingWriter{fail: true}
	s := &Sink{w: w}

	err := s.SetMultiline(context.Background(), "tags", []string{"one", "two"})
	if !errors.Is(err, errWriteFailed) {
		t.Fatalf("err = %v, want the writer's own failure", err)
	}

	if got := w.body.String(); got != "" {
		t.Errorf("a failed write left %q behind", got)
	}
}

// TestSetMultiline_IssuesExactlyOneWrite pins the mechanism the test above
// depends on, because a success-path test cannot tell one write from four.
//
// One write is not an atomic write, and the package comment says so: the
// operating system may still commit a prefix of a single write call, and
// nothing at this layer can take those bytes back out of a shared file. What
// this pins is the part that is this package's to control -- it never emits the
// opener as a step of its own and then fails.
func TestSetMultiline_IssuesExactlyOneWrite(t *testing.T) {
	w := &recordingWriter{}
	s := &Sink{w: w}

	if err := s.SetMultiline(context.Background(), "tags", []string{"one", "two", "three"}); err != nil {
		t.Fatal(err)
	}

	if w.writes != 1 {
		t.Errorf("the heredoc took %d writes, want 1", w.writes)
	}
}

// TestBuildHeredoc_RefusesALineEqualToTheDelimiter makes the "cannot collide"
// claim a property of the code rather than of the odds.
//
// Through SetMultiline the delimiter is 16 bytes from crypto/rand and no
// caller can supply a line matching it -- which is the design, and also why
// the guard is unreachable from outside the package. Calling the builder with
// a chosen delimiter is what lets the refusal be executed at all.
func TestBuildHeredoc_RefusesALineEqualToTheDelimiter(t *testing.T) {
	const delim = "EOF_deadbeef"

	if _, err := buildHeredoc("tags", delim, []string{"one", delim, "two"}); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// A line that merely contains the delimiter is content, not a terminator:
	// the runner ends the block on a line equal to it.
	got, err := buildHeredoc("tags", delim, []string{"prefix " + delim + " suffix"})
	if err != nil {
		t.Fatalf("a line containing the delimiter was refused: %v", err)
	}

	if want := "tags<<" + delim + "\nprefix " + delim + " suffix\n" + delim + "\n"; got != want {
		t.Errorf("block = %q, want %q", got, want)
	}
}
