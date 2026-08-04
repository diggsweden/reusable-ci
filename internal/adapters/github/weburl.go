// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import "fmt"

// ReleaseWebURL implements provider.WebURLBuilder.
func (p *Provider) ReleaseWebURL(server, repo, version string) string {
	return fmt.Sprintf("%s/%s/releases/tag/%s", server, repo, version)
}

// PackagesWebURL implements provider.WebURLBuilder.
func (p *Provider) PackagesWebURL(server, repo string) string {
	return fmt.Sprintf("%s/%s/packages", server, repo)
}
