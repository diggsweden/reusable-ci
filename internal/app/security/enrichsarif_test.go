// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

	fsys := testfs.NewReal(t)
	missing := fsys.Path("missing.sarif")

	err := appsecurity.EnrichGitHubSARIFFile(io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.EnrichGitHubSARIFInput{
		Path: missing,
	})
	if err != nil {
		t.Errorf("expected nil error on missing file, got: %v", err)
	}

	if !strings.Contains(stderr.String(), "::warning::SARIF file not found") {
		t.Errorf("missing warning:\n%s", stderr.String())
	}

	// A skip is a skip: nothing is written where the input was expected.
	if _, statErr := os.Stat(missing); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("the skipped path now exists, stat err = %v", statErr)
	}
}

func TestEnrichGitHubSARIFFile_EmptyPathErrors(t *testing.T) {
	var stderr bytes.Buffer

	err := appsecurity.EnrichGitHubSARIFFile(io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.EnrichGitHubSARIFInput{})
	// ErrUsage (exit 2), not the missing-file skip above: an empty --sarif-file is
	// a broken invocation, while a path that simply is not there is a no-op.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(stderr.String(), "::error::SARIF file is required") {
		t.Errorf("missing annotation:\n%s", stderr.String())
	}
}

// TestEnrichGitHubSARIFFile_StdinSentinelIsRefused pins that "-" is not an
// input here: the enriched document is written back to the input path, so
// accepting stdin would create a file literally named "-".
//
// The check runs in an owned working directory. It used to stat "-" in the
// package's source directory, so a stray file there failed it and a write
// anywhere else went unseen. Here a "-" canary with known bytes is seeded
// first: the refusal must leave it byte for byte, which also catches a write
// that truncated or replaced an existing file rather than creating one.
//
// Not parallel: changes the working directory.
func TestEnrichGitHubSARIFFile_StdinSentinelIsRefused(t *testing.T) {
	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)
	fsys.Chdir()

	const canary = "seeded canary, must survive\n"

	fsys.WriteFile("-", []byte(canary))

	err := appsecurity.EnrichGitHubSARIFFile(io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.EnrichGitHubSARIFInput{Path: "-"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if got := string(fsys.ReadFile("-")); got != canary {
		t.Errorf("the file named \"-\" was rewritten: %q", got)
	}

	entries, readErr := os.ReadDir(".")
	if readErr != nil {
		t.Fatal(readErr)
	}

	if len(entries) != 1 {
		t.Errorf("the refusal left %d entries in the working directory, want only the canary", len(entries))
	}
}

// TestEnrichGitHubSARIFFile_MalformedFileIsBadInputAndLeftAlone covers a
// truncated upload artifact: it is the caller's input that is wrong, so the
// error says so, and the file is not rewritten or replaced.
func TestEnrichGitHubSARIFFile_MalformedFileIsBadInputAndLeftAlone(t *testing.T) {
	t.Parallel()

	const body = `{"runs":[{"results":[`

	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("results.sarif", []byte(body))

	var out bytes.Buffer

	err := appsecurity.EnrichGitHubSARIFFile(&out, io.Discard, output.Annotator{}, appsecurity.EnrichGitHubSARIFInput{Path: path})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("err = %v, want ErrMalformedInput", err)
	}

	if got := string(fsys.ReadFile("results.sarif")); got != body || out.Len() != 0 {
		t.Errorf("file = %q, status = %q; want the file untouched and no status line", got, out.String())
	}

	entries, readErr := os.ReadDir(filepath.Dir(path))
	if readErr != nil || len(entries) != 1 {
		t.Errorf("directory holds %v (err %v), want only the input file", entries, readErr)
	}
}
