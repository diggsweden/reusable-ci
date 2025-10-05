// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func TestGoBuild_RendersMetadataLiterally(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, text string }{
		{"heading injection", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED "},
		{"inline syntax", "[link](https://evil.invalid) <b>&amp;</b> \\| `` *_~\t\r\n", "&#91;link&#93;(https&#58;//evil.invalid) &#60;b&#62;&#38;amp;&#60;/b&#62; &#92;&#124; &#96;&#96; &#42;&#95;&#126;   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}

			in := appsummary.GoBuildInput{
				Module: "module-" + tc.raw, BinaryName: "binary-" + tc.raw,
				Version: "version-" + tc.raw, Platforms: "platforms-" + tc.raw,
			}
			if err := appsummary.GoBuild(t.Context(), sink, in); err != nil {
				t.Fatal(err)
			}

			want := fmt.Sprintf("## Go Build Summary\n- **Module:** module-%s\n- **Binary:** binary-%s\n- **Version:** version-%s\n- **Platforms:** platforms-%s\n- **Tests:** go test ./...\n", tc.text, tc.text, tc.text, tc.text)
			if got := sink.buf.String(); got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

func TestGoBuild_RendersSummary(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.GoBuild(context.Background(), sink, appsummary.GoBuildInput{BinaryName: "app", Module: "github.com/org/app", Platforms: "linux/amd64", Version: "1.2.3", SkipTests: true}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	// Whole rows. "app" on its own was already satisfied by the module path,
	// so the binary row was never really checked, and Platforms/Version were
	// supplied by the fixture but asserted nowhere.
	got := sink.buf.String()
	for _, want := range []string{
		"## Go Build Summary",
		"- **Module:** github.com/org/app",
		"- **Binary:** app",
		"- **Version:** 1.2.3",
		"- **Platforms:** linux/amd64",
		"- **Tests:** skipped",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

// TestGoBuild_TestsRowNamesTheCommandWhenNotSkipped is the other half of the
// SkipTests branch: the row carries the command that actually ran, so a
// reader of the summary can tell "tests ran" from "tests were skipped".
func TestGoBuild_TestsRowNamesTheCommandWhenNotSkipped(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.GoBuild(context.Background(), sink, appsummary.GoBuildInput{
		BinaryName: "app",
		Module:     "github.com/org/app",
		Platforms:  "linux/amd64",
		Version:    "1.2.3",
		SkipTests:  false,
	}); err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	if !strings.Contains(got, "- **Tests:** go test ./...") {
		t.Errorf("missing executed-tests row in %s", got)
	}

	if strings.Contains(got, "- **Tests:** skipped") {
		t.Errorf("unexpected skipped row in %s", got)
	}
}
