// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package planfile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
)

// TestPlanScopedVerbsResolveFromPlan proves, for every plan-wired verb, that
// a representative flag resolves from a $REUSABLE_CI_PLAN plan file written
// under the verb's scope string (the command path). It walks the REAL
// assembled command tree, so a scope const drifting from its command path,
// or a flag losing its planfile source, fails here.
func TestPlanScopedVerbsResolveFromPlan(t *testing.T) {
	tests := []struct {
		scope string // plan scope == command path under the root
		flag  string // representative flag fed from the plan
	}{
		{scope: "container build", flag: "context"},
		{scope: "container build-push-oci-image", flag: "title"},
		// repository is cienv-chained; proves planfile.Chain wiring.
		{scope: "container build-push-oci-image", flag: "repository"},
		{scope: "container metadata", flag: "tag-rules"},
		{scope: "release assemble-dist", flag: "path"},
		{scope: "release sign", flag: "checksums-file"},
		// method comes from the shared signMethodFlags set.
		{scope: "release sign", flag: "method"},
		{scope: "release provenance", flag: "workflow"},
		// oidc-issuer comes from the shared signflags.Cosign set.
		{scope: "release provenance", flag: "oidc-issuer"},
		{scope: "toolchain install-mise", flag: "dest-dir"},
		{scope: "toolchain install-mise-tools", flag: "tools"},
		{scope: "toolchain install-changelog-renderer", flag: "mise-base-url"},
		{scope: "toolchain setup-mise-env", flag: "cache"},
	}

	for _, tc := range tests {
		t.Run(tc.scope+" --"+tc.flag, func(t *testing.T) {
			const want = "from-the-plan"

			raw, err := json.Marshal(map[string]map[string]string{tc.scope: {tc.flag: want}})
			require.NoError(t, err)

			path := filepath.Join(t.TempDir(), "plan.json")
			require.NoError(t, os.WriteFile(path, raw, 0o600))
			t.Setenv(planfile.EnvVar, path)

			flag := findFlag(t, cli.New(cli.BuildInfo{Version: "dev"}), tc.scope, tc.flag)

			got, ok := lookupThroughSources(t, flag)
			require.True(t, ok, "flag %q of %q resolved nothing from its sources", tc.flag, tc.scope)
			require.Equal(t, want, got, "flag %q of %q did not resolve from the plan file", tc.flag, tc.scope)
		})
	}
}

// findFlag walks the command tree along the space-separated scope path and
// returns the named flag of the leaf command.
func findFlag(t *testing.T, root *urfavecli.Command, scope, name string) urfavecli.Flag {
	t.Helper()

	cmd := root

	for _, part := range strings.Fields(scope) {
		var next *urfavecli.Command

		for _, sub := range cmd.Commands {
			if sub.Name == part {
				next = sub

				break
			}
		}

		require.NotNil(t, next, "scope %q: no subcommand %q under %q", scope, part, cmd.Name)
		cmd = next
	}

	for _, flag := range cmd.Flags {
		for _, n := range flag.Names() {
			if n == name {
				return flag
			}
		}
	}

	require.Failf(t, "flag not found", "scope %q has no flag %q", scope, name)

	return nil
}

// lookupThroughSources resolves a flag's value the way urfave/cli does when
// the flag is absent from argv: first source in the chain wins. With only
// the plan file set (no matching env), a hit MUST come from the plan.
func lookupThroughSources(t *testing.T, flag urfavecli.Flag) (string, bool) {
	t.Helper()

	for _, src := range flagSources(t, flag).Chain {
		if value, ok := src.Lookup(); ok {
			return value, true
		}
	}

	return "", false
}

// flagSources extracts the Sources chain from any urfave/cli flag type via
// reflection (all concrete flag types embed a FlagBase with a Sources field).
func flagSources(t *testing.T, flag urfavecli.Flag) urfavecli.ValueSourceChain {
	t.Helper()

	val := reflect.ValueOf(flag)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	field := val.FieldByName("Sources")
	require.True(t, field.IsValid(), "flag %v has no Sources field", flag.Names())

	chain, ok := field.Interface().(urfavecli.ValueSourceChain)
	require.True(t, ok, "flag %v Sources is not a ValueSourceChain", flag.Names())

	return chain
}
