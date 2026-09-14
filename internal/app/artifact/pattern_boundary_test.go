// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"bytes"
	"testing"

	appartifact "github.com/diggsweden/reusable-ci/v3/internal/app/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestArtifactPatternBoundary_CompleteRequestAndRefusals(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"", "release-*", "[ab]-?", "[", "bad\n*", "bad\x1b*", "bad\xff*"} {
		t.Run(pattern, func(t *testing.T) {
			in := provider.RunArtifactDownload{Pattern: pattern, MergeMultiple: pattern != "", Dir: t.TempDir(), RunID: "run-42", Repository: "owner/repository"}
			if pattern == "" {
				in.Name = "exact-artifact-name"
			}

			fake := &fakeArtifacts{info: provider.RunArtifactInfo{Name: "matched", ID: "artifact-7", FileCount: 3, Bytes: 99}}
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			_, err := appartifact.Download(t.Context(), fake, sink, &out, in)
			if pattern == "" || pattern == "release-*" || pattern == "[ab]-?" {
				if err != nil || fake.downloads != 1 || fake.gotDownload != in || sink.Single("artifact-id") != "artifact-7" || out.Len() == 0 {
					t.Fatalf("err=%v request=%+v output=%s", err, fake.gotDownload, &out)
				}
			} else if err == nil || fake.downloads != 0 || len(sink.Keys()) != 0 || out.Len() != 0 {
				t.Fatalf("err=%v request=%+v output=%s", err, fake.gotDownload, &out)
			}
		})
	}
}

func TestArtifactPatternBoundary_SelectorRefusals(t *testing.T) {
	t.Parallel()

	for _, in := range []provider.RunArtifactDownload{
		{Dir: "destination"}, {Dir: "destination", Name: "one", Pattern: "many-*"},
		{Name: "one"}, {Dir: "destination", Name: "bad\nname"},
	} {
		fake := &fakeArtifacts{}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		_, err := appartifact.Download(t.Context(), fake, sink, &out, in)
		if err == nil || fake.downloads != 0 || len(sink.Keys()) != 0 || out.Len() != 0 {
			t.Fatalf("input=%+v err=%v calls=%d outputs=%v text=%s", in, err, fake.downloads, sink.Keys(), &out)
		}
	}
}
