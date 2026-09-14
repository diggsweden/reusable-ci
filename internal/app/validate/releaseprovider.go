// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ReleaseProvider refuses GitHub Enterprise Server before a release workflow
// reaches actions/upload-artifact. The pinned v7 artifact action supports
// github.com only; failing here gives operators a stable configuration error
// instead of a late Node-action failure after builds have completed.
func ReleaseProvider(forge provider.ForgeAPI, serverURL string) error {
	switch provider.ClassifyReleaseWorkflowScope(forge, serverURL) {
	case provider.ReleaseWorkflowMalformedServerURL:
		// The value is not quoted back. A server URL that failed to parse is
		// exactly the one that may carry what should not be in it -- a token
		// in the userinfo of a URL that is otherwise broken -- and this message
		// goes to a CI log. The rule is what the operator needs.
		return fmt.Errorf("release provider server URL is not an absolute URL with a scheme and host (for example https://github.com): %w", errs.ErrInvalidConfig)
	case provider.ReleaseWorkflowUnsupportedInstance:
		host, _ := url.Parse(strings.TrimSpace(serverURL))

		return fmt.Errorf("release workflows do not support GitHub Enterprise Server %q because actions/upload-artifact@v7 is github.com-only; use github.com or a provider-native pipeline: %w",
			host.Hostname(), errs.ErrUnsupported)
	case provider.ReleaseWorkflowNotApplicable, provider.ReleaseWorkflowSupported:
		return nil
	}

	return nil
}
