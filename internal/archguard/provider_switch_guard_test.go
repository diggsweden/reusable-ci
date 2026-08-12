// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
)

// TestAppLayerDoesNotBranchOnPlatform enforces the Phase 1 invariant:
// application logic must not switch on a forge's identity. Forge
// differences belong behind the provider role interfaces (Describer,
// CapabilityReporter, TokenAdviser, …), so adding a new forge means
// implementing those interfaces in an adapter — not adding a `case` arm
// across internal/app.
//
// The single legitimate provider-construction switch lives in
// internal/cli/deps (providerFor); the runtime detection switch lives in
// internal/adapters/platform. Neither is under internal/app, so this guard stays
// green as long as the app layer asks the provider instead of branching
// on its name.
func TestAppLayerDoesNotBranchOnPlatform(t *testing.T) {
	t.Parallel()

	appDir := filepath.Join(reporoot.Path(t), "internal", "app")

	// Forbidden substrings: a case arm or direct comparison against a
	// concrete forge platform constant in non-test app code.
	forbidden := []string{
		"case provider.ForgeGitHub",
		"case provider.ForgeGitLab",
		"case provider.ForgeForgejo",
		"== provider.ForgeGitHub",
		"== provider.ForgeGitLab",
		"== provider.ForgeForgejo",
	}

	var offenders []string

	err := filepath.WalkDir(appDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // test reads repo-local source files.
		if readErr != nil {
			return readErr
		}

		src := string(body)
		for _, needle := range forbidden {
			if strings.Contains(src, needle) {
				rel, _ := filepath.Rel(reporoot.Path(t), path)
				offenders = append(offenders, rel+": "+needle)
			}
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"internal/app must not branch on forge identity — ask the provider "+
			"(Describer / Capabilities / role interfaces) instead. Offenders:\n%s",
		strings.Join(offenders, "\n"))
}

// TestDomainPlatformBranchingIsConfinedToPresentation pins the companion
// invariant: internal/domain is allowed to branch on forge identity ONLY in
// the deliberately-sanctioned presentation modules that render forge-specific
// display copy (summary labels, run URLs). Co-locating that copy here — rather
// than pushing UI strings into the I/O adapters — is the chosen trade-off, but
// it must stay contained: a new domain file that switches on a platform
// constant should force a conscious decision (refactor behind a provider role,
// or extend this allowlist with a justification), not slip in unnoticed.
//
// This makes the boundary the app-layer guard above already implies EXPLICIT
// and drift-proof, in the same guardrail-test style the repo uses elsewhere
// (workflow-contract, docs-sync, ledger-dry-run).
func TestDomainPlatformBranchingIsConfinedToPresentation(t *testing.T) {
	t.Parallel()

	// repo-relative path -> why the forge-specific branching is acceptable here.
	allowed := map[string]string{
		"internal/domain/security/opengrep.go": "code-scanning summary label/note differ per forge surface",
		"internal/domain/summary/urls.go":      "run/commit URL shapes differ per forge",
	}

	branches := []string{
		"case provider.ForgeGitHub",
		"case provider.ForgeGitLab",
		"case provider.ForgeForgejo",
		"== provider.ForgeGitHub",
		"== provider.ForgeGitLab",
		"== provider.ForgeForgejo",
	}

	domainDir := filepath.Join(reporoot.Path(t), "internal", "domain")

	var offenders []string

	err := filepath.WalkDir(domainDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // test reads repo-local source files.
		if readErr != nil {
			return readErr
		}

		rel, _ := filepath.Rel(reporoot.Path(t), path)
		if _, ok := allowed[filepath.ToSlash(rel)]; ok {
			return nil
		}

		src := string(body)
		for _, needle := range branches {
			if strings.Contains(src, needle) {
				offenders = append(offenders, filepath.ToSlash(rel)+": "+needle)
			}
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"internal/domain may branch on forge identity ONLY in the sanctioned "+
			"presentation modules (see the `allowed` map). Either refactor this "+
			"behind a provider role or add the file with a justification. Offenders:\n%s",
		strings.Join(offenders, "\n"))
}
