// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestProbeBotPermissions_OnlyARefusalIsAMissingPermission holds apart the two
// answers a probe can give. A forge that refused a probe, or hid the resource,
// reports that permission missing and nothing else; a forge that could not
// answer fails the whole check with the probe named and the failure's class
// kept, so an outage never reaches the operator as a token that needs more
// scope.
func TestProbeBotPermissions_OnlyARefusalIsAMissingPermission(t *testing.T) {
	t.Parallel()

	granted := func() error { return nil }
	answer := func(err error) func() error { return func() error { return err } }
	denied := answer(fmt.Errorf("HTTP 403: %w", errs.ErrPermissionDenied))
	hidden := answer(fmt.Errorf("HTTP 404: %w", errs.ErrMissingInput))

	for _, tc := range []struct {
		name                 string
		user, repo, branches func() error
		want                 provider.BotPermissions
	}{
		{"all granted", granted, granted, granted, provider.BotPermissions{UserAccessible: true, RepoAccessible: true, BranchesAccessible: true}},
		{"user denied", denied, granted, granted, provider.BotPermissions{RepoAccessible: true, BranchesAccessible: true}},
		{"repository hidden", granted, hidden, granted, provider.BotPermissions{UserAccessible: true, BranchesAccessible: true}},
		{"branches denied", granted, granted, denied, provider.BotPermissions{UserAccessible: true, RepoAccessible: true}},
		{"all refused", denied, hidden, denied, provider.BotPermissions{}},
	} {
		got, err := provider.ProbeBotPermissions(tc.user, tc.repo, tc.branches)
		if err != nil || got == nil || *got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}

	for _, tc := range []struct {
		name                 string
		user, repo, branches func() error
		class                error
		probe                string
	}{
		{"unavailable repository", granted, answer(fmt.Errorf("HTTP 503: %w", errs.ErrDependencyUnavailable)), granted, errs.ErrDependencyUnavailable, "repository probe: "},
		{"rate-limited branches", granted, granted, answer(fmt.Errorf("HTTP 429: %w", errs.ErrRateLimited)), errs.ErrRateLimited, "branches probe: "},
		{"timed-out user", answer(context.DeadlineExceeded), granted, granted, context.DeadlineExceeded, "user probe: "},
		{"unclassified beside a refusal", denied, answer(io.ErrUnexpectedEOF), denied, io.ErrUnexpectedEOF, "repository probe: unexpected EOF"},
	} {
		got, err := provider.ProbeBotPermissions(tc.user, tc.repo, tc.branches)
		if err == nil || got != nil {
			t.Errorf("%s: got %+v, %v; want a failure and no permissions", tc.name, got, err)

			continue
		}

		if !errors.Is(err, tc.class) {
			t.Errorf("%s: %v lost its class %v", tc.name, err, tc.class)
		}

		if errors.Is(err, errs.ErrPermissionDenied) || errors.Is(err, errs.ErrMissingInput) {
			t.Errorf("%s: %v reads as a refusal", tc.name, err)
		}

		if !strings.Contains(err.Error(), tc.probe) {
			t.Errorf("%s: %v does not name %q", tc.name, err, tc.probe)
		}
	}
}
