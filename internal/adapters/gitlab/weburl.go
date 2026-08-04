// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import "fmt"

// ReleaseWebURL implements provider.WebURLBuilder. GitLab routes project pages
// under "/-/" to keep them clear of the group/project namespace.
func (p *Provider) ReleaseWebURL(server, repo, version string) string {
	return fmt.Sprintf("%s/%s/-/releases/%s", server, repo, version)
}

// PackagesWebURL implements provider.WebURLBuilder.
func (p *Provider) PackagesWebURL(server, repo string) string {
	return fmt.Sprintf("%s/%s/-/packages", server, repo)
}
