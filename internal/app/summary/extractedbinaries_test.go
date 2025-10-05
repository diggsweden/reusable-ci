// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestExtractedBinaries_RendersMetadataAndPathsLiterally(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, text string }{
		{"heading injection", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED "},
		{"inline syntax", "[link](https://evil.invalid) <b>&amp;</b> \\| `` *_~\t\r\n", "&#91;link&#93;(https&#58;//evil.invalid) &#60;b&#62;&#38;amp;&#60;/b&#62; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The parent and filename are independently hostile; contents must never be read or changed.
			root := t.TempDir()

			parent := filepath.Join(root, "parent\n\n## B8-INJECTED\n`|<b>&[x]*\\")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(parent, "file\r\n`|<i>&[y]_\\")
			if err := os.WriteFile(path, []byte("file-content-canary\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			sink := &fakeSummarySink{}
			if err := appsummary.ExtractedBinaries(t.Context(), sink, appsummary.ExtractedBinariesInput{
				Dir: parent, DisplayName: "display-" + tc.raw, Platform: "platform-" + tc.raw,
				ExtractTarget: "stage-" + tc.raw, ArtifactName: "artifact-" + tc.raw, ExpectedNames: "expected-" + tc.raw,
			}); err != nil {
				t.Fatal(err)
			}

			want := fmt.Sprintf("### Extracted Binaries - display-%s (platform-%s)\n\n- **Stage:** stage-%s\n- **Artifact:** artifact-%s\n- **Platform:** platform-%s\n- **Binaries:** expected-%s\n\nFiles in extracted-binaries/:\n\n", tc.text, tc.text, tc.text, tc.text, tc.text, tc.text) +
				"<code>" + strings.ReplaceAll(root, "_", "&#95;") + "/parent  &#35;&#35; B8-INJECTED &#96;&#124;&#60;b&#62;&#38;&#91;x&#93;&#42;&#92;/file  &#96;&#124;&#60;i&#62;&#38;&#91;y&#93;&#95;&#92;</code>\n"
			if got := sink.buf.String(); got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}

			body, err := os.ReadFile(path)
			if err != nil || string(body) != "file-content-canary\n" {
				t.Fatalf("file canary = %q, err = %v", body, err)
			}

			for _, item := range []struct {
				path string
				mode os.FileMode
			}{{parent, 0o700}, {path, 0o600}} {
				info, statErr := os.Stat(item.path)
				if statErr != nil {
					t.Fatal(statErr)
				}

				if info.Mode().Perm() != item.mode {
					t.Errorf("mode of %q = %v, want %v", item.path, info.Mode().Perm(), item.mode)
				}
			}
		})
	}
}

func TestExtractedBinaries_RawExpectedNamesOmission(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, expected, row string }{
		{"whitespace only", " \t\r\n", ""},
		{"NUL is not raw whitespace", "\x00", "- **Binaries:**  \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			if err := appsummary.ExtractedBinaries(t.Context(), sink, appsummary.ExtractedBinariesInput{
				Dir: t.TempDir(), DisplayName: "demo", Platform: "linux/amd64",
				ExtractTarget: "export-binary", ArtifactName: "demo-binaries", ExpectedNames: tc.expected,
			}); err != nil {
				t.Fatal(err)
			}

			want := "### Extracted Binaries - demo (linux/amd64)\n\n- **Stage:** export-binary\n" +
				"- **Artifact:** demo-binaries\n- **Platform:** linux/amd64\n" + tc.row +
				"\nFiles in extracted-binaries/:\n\n"
			if got := sink.buf.String(); got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestExtractedBinaries_RelativeBlockSyntaxIsLiteralCode(t *testing.T) {
	t.Chdir(t.TempDir())

	names := []string{"---", "- item", "1. item"}
	for _, name := range names {
		if err := os.WriteFile(name, []byte("relative-file-canary\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sink := &fakeSummarySink{}
	if err := appsummary.ExtractedBinaries(t.Context(), sink, appsummary.ExtractedBinariesInput{
		Dir: ".", DisplayName: "demo", Platform: "linux/amd64",
		ExtractTarget: "export-binary", ArtifactName: "demo-binaries",
	}); err != nil {
		t.Fatal(err)
	}

	want := "### Extracted Binaries - demo (linux/amd64)\n\n- **Stage:** export-binary\n" +
		"- **Artifact:** demo-binaries\n- **Platform:** linux/amd64\n\nFiles in extracted-binaries/:\n\n" +
		"`- item`\n`---`\n`1. item`\n"
	if got := sink.buf.String(); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	for _, name := range names {
		body, err := os.ReadFile(name)
		if err != nil || string(body) != "relative-file-canary\n" {
			t.Fatalf("file %q = %q, err = %v", name, body, err)
		}

		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}

		if info.Mode().Perm() != 0o600 {
			t.Errorf("file %q mode = %v, want 0600", name, info.Mode().Perm())
		}
	}
}

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

	want := "### Extracted Binaries - demo (linux/amd64)\n\n- **Stage:** export-binary\n- **Artifact:** demo-binaries-amd64\n- **Platform:** linux/amd64\n- **Binaries:** demo\n\nFiles in extracted-binaries/:\n\n`" + fsys.Path("bin", "a") + "`\n"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	if strings.Contains(got, fsys.Path("bin", "b")) {
		t.Errorf("summary should be bounded, got %s", got)
	}
}
