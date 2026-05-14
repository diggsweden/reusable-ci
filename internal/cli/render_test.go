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
		"build", "ci", "config", "container", "plan", "publish",
		"release", "sbom", "security", "summary", "validate", "version",
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
		"### `reusable-ci security upload-sarif`",
		"### `reusable-ci summary maven-build`",
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
	for _, group := range []string{"build", "ci", "container", "sbom", "security"} {
		if !strings.Contains(toc, "[`"+group+"`]") {
			t.Errorf("ToC missing link to %q", group)
		}
	}
}

func TestRender_EscapesPipeInUsage(t *testing.T) {
	t.Parallel()
	cmd := &urfavecli.Command{
		Name: "demo",
		Commands: []*urfavecli.Command{
			{
				Name:  "sub",
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
