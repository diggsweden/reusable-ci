// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestFindContainerSBOM_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	target := "myapp-1.2.3-abc1234-analyzed-container-sbom.spdx.json"
	fsys.WriteFile(target, []byte("{}"))

	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	err := appsbom.FindContainerSBOM(context.Background(), sink, &out, io.Discard, output.Annotator{}, appsbom.FindContainerSBOMInput{Dir: dir})
	if err != nil {
		t.Fatalf("FindContainerSBOM: %v", err)
	}

	if got := sink.Single("sbom-file"); got != target {
		t.Errorf("sbom-file = %q, want %q", got, target)
	}

	if !strings.Contains(out.String(), "Found SBOM file: "+target) {
		t.Errorf("missing log line:\n%s", out.String())
	}
}

func TestFindContainerSBOM_NoneMatchesErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	// Decoy: a non-spdx SBOM file should not match.
	fsys.WriteFile("demo-analyzed-container-sbom.cyclonedx.json", []byte("{}"))

	var stderr bytes.Buffer

	err := appsbom.FindContainerSBOM(context.Background(), fakeoutputsink.New(t), io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsbom.FindContainerSBOMInput{Dir: dir})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "::error::No container SBOM file found") {
		t.Errorf("missing error line:\n%s", stderr.String())
	}
}
