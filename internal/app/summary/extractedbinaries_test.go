// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestExtractedBinaries_RendersBoundedFileList(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("bin/b", []byte("b"))
	fsys.WriteFile("bin/a", []byte("a"))

	sink := &fakeSummarySink{}

	if err := appsummary.ExtractedBinaries(context.Background(), sink, appsummary.ExtractedBinariesInput{
		Dir:           fsys.Path("bin"),
		ArtifactName:  "demo-binaries-amd64",
		DisplayName:   "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ExtractTarget: "export-binary",
		ExpectedNames: "demo",
		Platform:      "linux/amd64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Limit:         1,
	}); err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{"Extracted Binaries", "demo-binaries-amd64", "linux/amd64", fsys.Path("bin", "a")} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}

	if strings.Contains(got, fsys.Path("bin", "b")) {
		t.Errorf("summary should be bounded, got %s", got)
	}
}
