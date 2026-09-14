// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestEventPolicyBoundary_RejectsCommentsAndBypasses(t *testing.T) {
	t.Parallel()

	const head = "on:\n  workflow_call:\npermissions:\n  packages: write\njobs:\n  publish:\n    steps:\n"

	const secret = "      - env:\n          TOKEN: ${{ github.token }}\n        run: publish\n" //nolint:gosec // workflow expression fixture, not a credential.
	for _, guard := range []string{"# reusable-ci validate event-context", "false && reusable-ci validate event-context", "echo reusable-ci validate event-context", "reusable-ci validate event-context || true"} {
		body := head + "      - run: |\n          " + guard + "\n" + secret
		require.NotEmpty(t, eventContextViolations([]byte(body), false), guard)
	}

	good := head + "      - run: reusable-ci validate event-context\n" + secret
	require.Empty(t, eventContextViolations([]byte(good), false))

	for _, condition := range []string{"always()", "AlWaYs ()", "success() || true", "failure()"} {
		bypass := strings.Replace(good, "      - env:", "      - if: "+condition+"\n        env:", 1)
		require.NotEmpty(t, eventContextViolations([]byte(bypass), false), condition)
	}

	require.NotEmpty(t, eventContextViolations([]byte(good), true), "forwarder may not consume secrets directly")

	for _, bypass := range []string{"        if: false\n", "        continue-on-error: true\n", "        shell: bash {0} || true\n"} {
		body := head + "      - run: reusable-ci validate event-context\n" + bypass + secret
		require.NotEmpty(t, eventContextViolations([]byte(body), false))
	}

	readonly := strings.Replace(head, "packages: write", "contents: read", 1) + secret
	require.Empty(t, eventContextViolations([]byte(readonly), false), "read-only GitHub token is not a publishing credential")

	presence := head + "      - if: always()\n        env:\n          HAS_KEY: ${{ secrets.SIGN_KEY != '' }}\n        run: report\n"
	require.Empty(t, eventContextViolations([]byte(presence), false))
	require.NotEmpty(t, eventContextViolations([]byte(strings.Replace(presence, "!= ''", "!= '' && secrets.SIGN_KEY", 1)), false))
}

func TestEventPolicyBoundary_RequiresGuardDependencies(t *testing.T) {
	t.Parallel()

	const (
		head   = "on:\n  workflow_call:\njobs:\n  guard:\n    steps:\n      - run: reusable-ci validate event-context\n  publish:\n"
		secret = "    steps:\n      - env:\n          TOKEN: ${{ secrets.SIGN_KEY }}\n        run: publish\n"
	)
	require.NotEmpty(t, eventContextViolations([]byte(head+secret), false))
	require.Empty(t, eventContextViolations([]byte(head+"    needs: guard\n"+secret), false))
	require.NotEmpty(t, eventContextViolations([]byte(head+"    needs: guard\n    if: always()\n"+secret), false))
	transitive := strings.Replace(head, "  publish:\n", "  bridge:\n    needs: guard\n    steps:\n      - run: true\n  publish:\n    needs: bridge\n", 1)
	require.Empty(t, eventContextViolations([]byte(transitive+secret), false))
}

func TestSupplyChainBoundary_PinsEveryExternalUse(t *testing.T) {
	t.Parallel()

	for _, use := range []string{"actions/checkout@main", "org/repo/.github/workflows/build.yml@v1", "docker://alpine:latest"} {
		body := []byte("jobs:\n  job:\n    uses: " + use + "\n")
		require.NotEmpty(t, supplyChainViolations(body))
	}

	for _, use := range []string{"actions/checkout@" + strings.Repeat("a", 40), "./.github/workflows/build.yml", "docker://registry.example/app@sha256:" + strings.Repeat("b", 64)} {
		require.Empty(t, supplyChainViolations([]byte("jobs:\n  job:\n    steps:\n      - uses: "+use+"\n")))
	}

	require.NotEmpty(t, supplyChainViolations([]byte("permissions: write-all\njobs: {}\n")))
	require.NotEmpty(t, supplyChainViolations([]byte("permissions: read-all\njobs:\n  job:\n    permissions: write-all\n")))
}

func TestWorkflowBoundary_DirectivesActionsAndRemote(t *testing.T) {
	t.Parallel()

	for _, directive := range []string{"from", "FrOm", "FROM\t"} {
		require.NotEmpty(t, unpinnedRuntimeStages(directive+" alpine:latest\n"))
	}

	require.Empty(t, unpinnedRuntimeStages("from alpine@sha256:"+strings.Repeat("a", 64)+" AS base\nFROM base\n"))

	for _, action := range []string{"actions/upload-artifact", "Actions/Upload-Artifact", "ACTIONS/UPLOAD-ARTIFACT"} {
		body := "jobs:\n  job:\n    steps:\n      - uses: " + action + "@pinned\n        with:\n          path: .\n"
		require.Len(t, auditUploadArtifactPaths(t, "fixture.yml", []byte(body)), 1)
	}

	const body = "jobs:\n  parse-config:\n    steps:\n      - name: Resolve reusable-ci ref to commit SHA\n        env:\n          REMOTE_URL: https://github.com/diggsweden/reusable-ci\n        run: reusable-ci platform resolve-ref\n"
	require.True(t, canonicalResolveStep([]byte(body)))

	for _, command := range []string{"# reusable-ci platform resolve-ref", "REMOTE_URL=https://evil.invalid reusable-ci platform resolve-ref", "reusable-ci platform resolve-ref --remote https://evil.invalid"} {
		require.False(t, canonicalResolveStep([]byte(strings.Replace(body, "run: reusable-ci platform resolve-ref", "run: |\n          "+command, 1))))
	}

	require.False(t, canonicalResolveStep([]byte("env:\n  BASH_ENV: evil.sh\n"+body)))
}
