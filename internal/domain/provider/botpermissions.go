// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"errors"
	"fmt"
	"sync"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ProbeBotPermissions runs the three bot permission probes concurrently and
// reports each answer. A probe the forge refused, as permission denied (401,
// 403) or as missing (404, which is how a forge hides a repository the token
// cannot see), is a missing permission. Any other failure, such as an
// unavailable or rate-limited forge, a timeout or a cancelled request, is
// returned instead: reporting an outage as a missing permission tells the
// operator to widen a scope that is not the problem, and tells a retry loop
// the failure is permanent.
func ProbeBotPermissions(user, repo, branches func() error) (*BotPermissions, error) {
	var permissions BotPermissions

	probes := []struct {
		name    string
		probe   func() error
		granted *bool
	}{
		{"user", user, &permissions.UserAccessible},
		{"repository", repo, &permissions.RepoAccessible},
		{"branches", branches, &permissions.BranchesAccessible},
	}

	failures := make([]error, len(probes))

	var wg sync.WaitGroup

	for index, probe := range probes {
		wg.Go(func() {
			err := probe.probe()

			switch {
			case err == nil:
				*probe.granted = true
			case errors.Is(err, errs.ErrPermissionDenied), errors.Is(err, errs.ErrMissingInput):
			default:
				failures[index] = fmt.Errorf("%s probe: %w", probe.name, err)
			}
		})
	}

	wg.Wait()

	if err := errors.Join(failures...); err != nil {
		return nil, err
	}

	return &permissions, nil
}
