// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"fmt"
	"net/url"
	"strings"
)

// resourceLine renders one bullet of a summary's Resources section.
//
// Only an absolute http(s) URL becomes a Markdown link. Anything else -- an
// empty run URL, a link the platform cannot build -- is rendered as plain text
// saying the resource is not available. These lines used to interpolate
// whatever they were given into the link destination, so a platform without a
// web UI produced "[Workflow Run]()" and "[Packages]((packages))". A URL
// containing characters that end a Markdown link destination is refused the
// same way rather than allowed to close the link early and append text of its
// own, and so is a URL carrying userinfo, which would publish a credential in
// the job summary.
func resourceLine(label, link string) string {
	parsed, err := url.Parse(link)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil ||
		strings.ContainsAny(link, " ()<>\t\r\n") {
		return fmt.Sprintf("- %s: not available\n", label)
	}

	return fmt.Sprintf("- [%s](%s)\n", label, link)
}
