// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"context"
	"fmt"

	"code.gitea.io/sdk/gitea"
)

const forgejoPackagePageSize = 50

// ListContainerPackageVersions returns every version for one container package.
func (p *Provider) ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error) {
	client, err := p.client(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]string, 0)

	for page := 1; ; page++ {
		packages, resp, err := client.ListPackageVersions(owner, "container", name, gitea.ListPackagesOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: forgejoPackagePageSize},
		})
		if err != nil {
			return nil, fmt.Errorf("forgejo list container package versions %s/%s: %w", owner, name, classifyErr(resp, err))
		}

		if len(packages) == 0 {
			break
		}

		versions = appendContainerPackageVersions(versions, packages, name)

		if resp == nil || resp.NextPage == 0 {
			break
		}
	}

	return versions, nil
}

// appendContainerPackageVersions appends the versions of every container
// package matching name to versions.
func appendContainerPackageVersions(versions []string, packages []*gitea.Package, name string) []string {
	for _, pkg := range packages {
		if pkg != nil && pkg.Type == "container" && pkg.Name == name && pkg.Version != "" {
			versions = append(versions, pkg.Version)
		}
	}

	return versions
}
