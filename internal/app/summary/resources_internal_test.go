// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import "testing"

// TestResourceLine_LinksOnlyARealURL covers the renderer the three summaries
// share. With no web UI the Resources section used to read
// "[Release]((release: v1.2.3))", "[Packages]((packages))" and
// "[Workflow Run]()": malformed links in the summary a reviewer reads.
func TestResourceLine_LinksOnlyARealURL(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ link, want string }{
		"an https URL":          {link: "https://forge.example/owner/repo/releases/tag/v1", want: "- [Release](https://forge.example/owner/repo/releases/tag/v1)\n"},
		"an http URL":           {link: "http://forge.internal/run/9", want: "- [Release](http://forge.internal/run/9)\n"},
		"empty":                 {link: "", want: "- Release: not available\n"},
		"a relative path":       {link: "/owner/repo/releases/tag/v1", want: "- Release: not available\n"},
		"another scheme":        {link: "javascript:alert(1)", want: "- Release: not available\n"},
		"no host":               {link: "https://", want: "- Release: not available\n"},
		"a closing parenthesis": {link: "https://forge.example/v1)[evil](https://evil.example", want: "- Release: not available\n"},
		"embedded whitespace":   {link: "https://forge.example/a b", want: "- Release: not available\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := resourceLine("Release", tc.link); got != tc.want {
				t.Errorf("resourceLine(%q) = %q, want %q", tc.link, got, tc.want)
			}
		})
	}
}
