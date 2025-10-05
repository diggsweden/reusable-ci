// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"fmt"
	"strings"
)

// ReleaseWebURL implements provider.WebURLBuilder. Release pages share GitHub's
// shape under Gitea routing.
func (p *Provider) ReleaseWebURL(server, repo, version string) string {
	return fmt.Sprintf("%s/%s/releases/tag/%s", server, repo, version)
}

// PackagesWebURL implements provider.WebURLBuilder. Packages are owner-scoped in
// the Gitea/Forgejo model, not repository-scoped, so the owner segment is
// projected out of the canonical "owner/repo".
func (p *Provider) PackagesWebURL(server, repo string) string {
	owner, _, _ := strings.Cut(repo, "/")

	return fmt.Sprintf("%s/%s/-/packages", server, owner)
}
