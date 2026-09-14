// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestLiteralText_EncodesInlineData(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, want string }{
		{"empty", "", ""},
		{"spaces", "  a  b  ", "  a  b  "},
		{"ordinary", "github.com/org/app v1.2.3-beta linux/amd64 generic/platform=iOS (42)", "github.com/org/app v1.2.3-beta linux/amd64 generic/platform=iOS (42)"},
		{"unicode", "R\u00e4ksm\u00f6rg\u00e5s \u65e5\u672c\u8a9e\u00a0ok", "R\u00e4ksm\u00f6rg\u00e5s \u65e5\u672c\u8a9e\u00a0ok"},
		{"controls", "a\x00\t\r\n\x1b\x7f\u0085b", "a       b"},
		{"heading", "demo\n\n## B8-INJECTED\n", "demo  &#35;&#35; B8-INJECTED "},
		{"html and entities", "<b title=\"x\">'&amp;&#96;</b>", "&#60;b title=&#34;x&#34;&#62;&#39;&#38;amp;&#38;&#35;96;&#60;/b&#62;"},
		{"links and emphasis", "![x](https://evil.invalid) [ref][id] *b* _i_ ~~s~~", "&#33;&#91;x&#93;(https&#58;//evil.invalid) &#91;ref&#93;&#91;id&#93; &#42;b&#42; &#95;i&#95; &#126;&#126;s&#126;&#126;"},
		{"delimiter composition", "\\| \\\\| ` `` ``` \\* \\[", "&#92;&#124; &#92;&#92;&#124; &#96; &#96;&#96; &#96;&#96;&#96; &#92;&#42; &#92;&#91;"},
		{"autolinks", "https://evil.invalid www.evil.invalid WWW.evil.invalid x@evil.invalid <https://evil.invalid>", "https&#58;//evil.invalid www&#46;evil.invalid WWW&#46;evil.invalid x&#64;evil.invalid &#60;https&#58;//evil.invalid&#62;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := summary.LiteralText(tc.raw); got != tc.want {
				t.Errorf("LiteralText(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestInlineCode_ProducesCompleteLiteralCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, raw, want string }{
		{"ordinary", "app-v1.2.3.aab", "`app-v1.2.3.aab`"},
		{"unicode and internal spaces", "R\u00e4ksm\u00f6rg\u00e5s  \u65e5\u672c\u8a9e", "`R\u00e4ksm\u00f6rg\u00e5s  \u65e5\u672c\u8a9e`"},
		{"empty", "", "<code></code>"},
		{"one space", " ", "<code> </code>"},
		{"all spaces", "   ", "<code>   </code>"},
		{"leading space", " a", "<code> a</code>"},
		{"trailing space", "a ", "<code>a </code>"},
		{"both boundary spaces", " a ", "<code> a </code>"},
		{"controls", "a\x00\t\r\n\x1b\x7f\u0085b", "`a       b`"},
		{"boundary controls", "\ta\r\n", "<code> a  </code>"},
		{"only controls", "\t\r\n", "<code>   </code>"},
		{"backslash", "a\\b\\", "`a\\b\\`"},
		{"pipe", "a|b", "<code>a&#124;b</code>"},
		{"backslashes and pipe", "a\\|b\\\\|c", "<code>a&#92;&#124;b&#92;&#92;&#124;c</code>"},
		{"backtick", "`", "<code>&#96;</code>"},
		{"repeated backticks", "``x```y``", "<code>&#96;&#96;x&#96;&#96;&#96;y&#96;&#96;</code>"},
		{"raw data inside Markdown code", "<b>&amp;&#96;[x](https://evil.invalid) *_~", "`<b>&amp;&#96;[x](https://evil.invalid) *_~`"},
		{"encoded data inside HTML code", "`<b>&amp;&#96;[x](https://evil.invalid) *_~", "<code>&#96;&#60;b&#62;&#38;amp;&#38;&#35;96;&#91;x&#93;(https&#58;//evil.invalid) &#42;&#95;&#126;</code>"},
		{"URL in Markdown code", "https://evil.invalid", "`https://evil.invalid`"},
		{"autolinks inside HTML code", "www.evil.invalid x@evil.invalid|", "<code>www&#46;evil.invalid x&#64;evil.invalid&#124;</code>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := summary.InlineCode(tc.raw); got != tc.want {
				t.Errorf("InlineCode(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
