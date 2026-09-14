// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type snapshotClosedWriter struct{}

func (snapshotClosedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestSnapshotDeliveryBoundary_WriterErrorsAndMachineCommit(t *testing.T) {
	t.Parallel()

	for _, format := range []output.Format{output.FormatText, output.FormatJSON, output.FormatGitHub, output.FormatGitLab} {
		ops := &fakeGit{tags: []string{"v1.2.3"}, shortSHAOut: "abc1234"}
		sink := fakeoutputsink.New(t)
		in := appversion.GenerateSnapshotVersionInput{RefName: "branch", Format: format, Sink: sink}

		var out bytes.Buffer
		if err := appversion.GenerateSnapshotVersion(t.Context(), ops, &out, in); err != nil {
			t.Fatal(err)
		}

		want := "1.2.3-snapshot-branch-abc1234\n"
		if format == output.FormatJSON {
			want = "{\"version\":\"1.2.3-snapshot-branch-abc1234\"}\n"
		}

		if out.String() != want {
			t.Fatalf("bytes=%q", out.String())
		}

		committed := fakeoutputsink.New(t)

		in.Sink = committed
		if err := appversion.GenerateSnapshotVersion(t.Context(), ops, snapshotClosedWriter{}, in); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("err=%v", err)
		}

		if format == output.FormatGitHub || format == output.FormatGitLab {
			if committed.Single("snapshot-version") != "1.2.3-snapshot-branch-abc1234" {
				t.Fatal("machine commit lost")
			}
		}
	}
}
