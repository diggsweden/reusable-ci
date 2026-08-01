// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

//nolint:cyclop // round-trips and checks every enriched SARIF field.
func TestEnrichGitHubSARIFFile_WritesBack(t *testing.T) {
	fsys := testfs.NewReal(t)
	body := `{"runs":[{"results":[{"ruleId":"R","fingerprints":{"matchBasedId/v1":"m"}}]}]}`
	path := fsys.WriteFile("results.sarif", []byte(body))

	var out bytes.Buffer
	if err := appsecurity.EnrichGitHubSARIFFile(&out, io.Discard, output.Annotator{}, appsecurity.EnrichGitHubSARIFInput{Path: path}); err != nil {
		t.Fatalf("EnrichGitHubSARIFFile: %v", err)
	}

	if !strings.Contains(out.String(), "Enriched SARIF") {
		t.Errorf("missing status line:\n%s", out.String())
	}

	data := fsys.ReadFile("results.sarif")

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("doc.runs is not a non-empty array")
	}

	run0, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("doc.runs[0] is not an object")
	}

	results, ok := run0["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatal("doc.runs[0].results is not a non-empty array")
	}

	res, ok := results[0].(map[string]any)
	if !ok {
		t.Fatal("doc.runs[0].results[0] is not an object")
	}

	fp, ok := res["partialFingerprints"].(map[string]any)
	if !ok {
		t.Fatal("partialFingerprints missing or wrong shape")
	}

	if fp["primaryLocationLineHash"] != "m" {
		t.Errorf("hash = %v", fp["primaryLocationLineHash"])
	}
}

func TestEnrichGitHubSARIFFile_MissingFileSkips(t *testing.T) {
	var stderr bytes.Buffer

	err := appsecurity.EnrichGitHubSARIFFile(io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.EnrichGitHubSARIFInput{
		Path: "/nonexistent/path.sarif",
	})
	if err != nil {
		t.Errorf("expected nil error on missing file, got: %v", err)
	}

	if !strings.Contains(stderr.String(), "::warning::SARIF file not found") {
		t.Errorf("missing warning:\n%s", stderr.String())
	}
}

func TestEnrichGitHubSARIFFile_EmptyPathErrors(t *testing.T) {
	err := appsecurity.EnrichGitHubSARIFFile(io.Discard, io.Discard, output.Annotator{}, appsecurity.EnrichGitHubSARIFInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}
