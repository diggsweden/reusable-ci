// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestEnrichGitHubSARIFFile_WritesBack(t *testing.T) {
	fsys := testfs.NewReal(t)
	body := `{"runs":[{"results":[{"ruleId":"R","fingerprints":{"matchBasedId/v1":"m"}}]}]}`
	path := fsys.WriteFile("results.sarif", []byte(body))

	var stdout bytes.Buffer
	if err := appsecurity.EnrichGitHubSARIFFile(&stdout, io.Discard, output.Annotator{}, appsecurity.EnrichGitHubSARIFInput{Path: path}); err != nil {
		t.Fatalf("EnrichGitHubSARIFFile: %v", err)
	}
	if !strings.Contains(stdout.String(), "Enriched SARIF") {
		t.Errorf("missing status line:\n%s", stdout.String())
	}

	out := fsys.ReadFile("results.sarif")
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	runs := doc["runs"].([]any)
	res := runs[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	fp := res["partialFingerprints"].(map[string]any)
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
