// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"github.com/stretchr/testify/require"
	urfave "github.com/urfave/cli/v3"
	"testing"
)

func TestRenderBoundary_RootAndTypedFlagMetadata(t *testing.T) {
	t.Parallel()

	root := &urfave.Command{Name: "fixture", Flags: []urfave.Flag{&urfave.StringFlag{Name: "mode", Aliases: []string{"m"}, Required: true, Value: "auto", Sources: urfave.EnvVars("MODE")}, &urfave.BoolFlag{Name: "enabled", Value: true}, &urfave.IntFlag{Name: "count", Value: 42}, &urfave.StringFlag{Name: "hidden-secret", Hidden: true}}, Commands: []*urfave.Command{{Name: "child", Flags: []urfave.Flag{&urfave.BoolFlag{Name: "off", Value: false}}}}}

	text := Render(root)
	for _, fragment := range []string{"## Global options", "`--mode`, `--m`", "(required)", "`$MODE`", "string", "auto", "bool", "42", "`--off`"} {
		require.Contains(t, text, fragment)
	}

	require.NotContains(t, text, "hidden-secret")

	for _, row := range []string{"| `--mode`, `--m` | Sets mode. (required) | string | `\"auto\"` | `$MODE` |", "| `--enabled` | Sets enabled. | bool | `true` | n/a |", "| `--count` | Sets count. | int | `42` | n/a |", "| `--off` | Sets off. | bool | `false` | n/a |"} {
		require.Contains(t, text, row)
	}

	root.Flags = append(root.Flags, &urfave.StringFlag{Name: "new-root-option"})
	require.NotEqual(t, text, Render(root))
	require.Contains(t, Render(New(BuildInfo{Version: "test"})), "--no-color")
}

// TestRender_DescriptionFenceOutlastsItsContent covers the verbatim block. It
// was a fixed ``` fence, documented as safe because no description contained
// one, with nothing enforcing that; an EXAMPLE showing a fenced snippet would
// close the block and render the rest of the page as Markdown.
func TestRender_DescriptionFenceOutlastsItsContent(t *testing.T) {
	t.Parallel()

	root := &urfave.Command{Name: "fixture", Commands: []*urfave.Command{{
		Name:        "child",
		Description: "EXAMPLE:\n```yaml\nkey: value\n```\n",
	}}}

	require.Contains(t, Render(root), "## `fixture child`\n\n````\nEXAMPLE:\n```yaml\nkey: value\n```\n````\n")
}

// TestRender_EscapesExactlyTheSupportedMarkdown pins both escapers to the
// characters they claim, including what they leave alone.
func TestRender_EscapesExactlyTheSupportedMarkdown(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"list <build-dir>":   "list &lt;build-dir&gt;",
		"*.tgz and **bold**": `\*.tgz and \*\*bold\*\*`,
		"a | b":              "a | b",
		"snake_case [x] `y`": "snake_case [x] `y`",
		"line\nbreak":        "line\nbreak",
	} {
		require.Equal(t, want, escapeText(in), "escapeText(%q)", in)
	}

	for in, want := range map[string]string{
		"a | b":       `a \| b`,
		"line\nbreak": "line break",
		"<x> *y* `z`": "<x> *y* `z`",
		"||\n\n":      `\|\|  `,
	} {
		require.Equal(t, want, escapeCell(in), "escapeCell(%q)", in)
	}
}
