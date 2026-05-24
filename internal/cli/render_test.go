// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package cli_test

import (
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	internalcli "github.com/diggsweden/reusable-ci/internal/cli"
)

func TestRender_AgainstLiveRoot(t *testing.T) {
	t.Parallel()

	body := internalcli.Render(internalcli.New(internalcli.BuildInfo{Version: "test"}))

	// Smoke-test the top-level groups all appear.
	for _, group := range []string{
		"build", "config", "container", "plan", "platform", "publish",
		"release", "report", "sbom", "security", "validate", "version",
	} {
		if !strings.Contains(body, "## `reusable-ci "+group+"`") {
			t.Errorf("missing top-level group heading for %q", group)
		}
	}
	// A handful of representative subcommand headings.
	for _, want := range []string{
		"### `reusable-ci build maven`",
		"#### `reusable-ci build maven library`",
		"### `reusable-ci sbom generate`",
		"#### `reusable-ci security report upload-sarif`",
		"#### `reusable-ci report build maven`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing heading %q", want)
		}
	}
}

func TestRender_TableOfContentsHasOneEntryPerGroup(t *testing.T) {
	t.Parallel()

	body := internalcli.Render(internalcli.New(internalcli.BuildInfo{}))

	tocStart := strings.Index(body, "## Command groups")
	if tocStart < 0 {
		t.Fatal("no ToC")
	}

	firstHeading := strings.Index(body[tocStart:], "## `reusable-ci ")
	if firstHeading < 0 {
		t.Fatal("no group heading after ToC")
	}

	toc := body[tocStart : tocStart+firstHeading]
	for _, group := range []string{"build", "platform", "container", "sbom", "security"} {
		if !strings.Contains(toc, "[`"+group+"`]") {
			t.Errorf("ToC missing link to %q", group)
		}
	}

	if !strings.Contains(toc, "[`build`](#reusable-ci-build)") {
		t.Errorf("ToC link target does not match generated heading anchor:\n%s", toc)
	}
}

func TestRender_EscapesPipeInUsage(t *testing.T) {
	t.Parallel()

	cmd := &urfavecli.Command{
		Name: "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Commands: []*urfavecli.Command{
			{
				Name:  "sub", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Usage: "with a | pipe in it",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "x", Usage: "a | b"},
				},
			},
		},
	}

	body := internalcli.Render(cmd)
	if !strings.Contains(body, `a \| b`) {
		t.Errorf("pipe in flag usage not escaped:\n%s", body)
	}
}

func TestRender_EscapesMarkdownInUsageParagraphs(t *testing.T) {
	t.Parallel()

	cmd := &urfavecli.Command{
		Name: "demo",
		Commands: []*urfavecli.Command{
			{Name: "sub", Usage: "list *.tgz files under <build-dir>"},
		},
	}

	body := internalcli.Render(cmd)
	if !strings.Contains(body, `list \*.tgz files under &lt;build-dir&gt;`) {
		t.Errorf("markdown-sensitive usage text not escaped:\n%s", body)
	}
}

func TestRender_FillsMissingFlagDescription(t *testing.T) {
	t.Parallel()

	cmd := &urfavecli.Command{
		Name: "demo",
		Commands: []*urfavecli.Command{
			{
				Name: "sub",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "missing-usage"},
				},
			},
		},
	}

	body := internalcli.Render(cmd)
	if !strings.Contains(body, "| `--missing-usage` | Sets missing usage. | n/a |") {
		t.Errorf("missing fallback flag usage row:\n%s", body)
	}
}

func TestRender_EndsWithSingleNewline(t *testing.T) {
	t.Parallel()

	body := internalcli.Render(&urfavecli.Command{Name: "demo"})
	if !strings.HasSuffix(body, "\n") || strings.HasSuffix(body, "\n\n") {
		t.Errorf("rendered body must end with exactly one newline")
	}
}
