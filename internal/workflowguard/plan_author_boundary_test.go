// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPlanAuthorBoundary_DoesNotExemptOtherInvocations(t *testing.T) {
	t.Parallel()

	tree := cli.New(cli.BuildInfo{Version: "test"})
	scoped := planScopedCommandPaths(t, tree)

	const body = "jobs:\n  build:\n    env:\n      REUSABLE_CI_PLAN: .plan.json\n    steps:\n      - run: |\n          reusable-ci plan write --scope 'container build' --set context=.\n          reusable-ci container build\n      - run: reusable-ci container build\n"

	violations := auditFixture(t, tree, scoped, body)
	require.Len(t, violations, 1)
	require.Contains(t, violations[0], "container build")
}
