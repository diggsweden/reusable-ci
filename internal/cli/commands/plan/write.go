// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const defaultPlanPath = ".reusable-ci/plan.json"

func writeCmd() *cli.Command {
	return &cli.Command{
		Name:  "write",
		Usage: "write or extend the plan file that feeds command flags",
		Description: `The generated counterpart of hand-set env blocks: workflows compute
their values once, write them into the plan with this verb, and export
$REUSABLE_CI_PLAN so later commands read them (flag > plan > env >
default). Writing is STRICT: the scope must name a real command and
every key must be one of its flags, so a typo fails the workflow here
instead of silently falling through to an env var or default. The plan
is merged scope-by-scope, so each stage can add its own section, and
the file's sha256 is printed and emitted for auditing.

EXAMPLE:
   reusable-ci plan write --scope "container build" \
     --set context=. --set platform=linux/amd64 --set mode=push-by-digest`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "scope", Required: true, Sources: cli.EnvVars("PLAN_SCOPE"), Usage: `command path the values feed, e.g. "container build"`},
			&cli.StringSliceFlag{Name: "set", Required: true, Usage: "flag=value pair to record (repeatable)"},
			&cli.StringFlag{Name: "plan-file", Sources: cli.EnvVars(planfile.EnvVar), Value: defaultPlanPath, Usage: "plan file to create or merge into"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return runPlanWrite(ctx, cmd, dep)
			})
		},
	}
}

func runPlanWrite(ctx context.Context, cmd *cli.Command, dep *deps.Deps) error {
	scope := cmd.String("scope")

	target, err := resolveScopeCommand(cmd.Root(), scope)
	if err != nil {
		return err
	}

	values, err := parsePlanSets(cmd.StringSlice("set"), target, scope)
	if err != nil {
		return err
	}

	path := cmd.String("plan-file")

	plan, err := loadPlanForMerge(path)
	if err != nil {
		return err
	}

	scoped := plan[scope]
	if scoped == nil {
		scoped = map[string]any{}
	}

	for key, value := range values {
		scoped[key] = value
	}

	plan[scope] = scoped

	digest, err := writePlanFile(path, plan)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "plan: wrote %d value(s) for scope %q to %s (sha256 %s)\n", len(values), scope, path, digest)

	if err := dep.OutputSink.Set(ctx, "plan-file", path); err != nil {
		return err
	}

	return dep.OutputSink.Set(ctx, "plan-sha256", digest)
}

// resolveScopeCommand walks the live command tree so a plan scope can only
// name a command that actually exists.
func resolveScopeCommand(root *cli.Command, scope string) (*cli.Command, error) {
	current := root

	for _, part := range strings.Fields(scope) {
		next := findSubcommand(current, part)
		if next == nil {
			return nil, fmt.Errorf("plan: scope %q does not resolve to a command (no %q under %q): %w",
				scope, part, current.Name, errs.ErrUsage)
		}

		current = next
	}

	if current == root || hasRealSubcommands(current) {
		return nil, fmt.Errorf("plan: scope %q is not a leaf command: %w", scope, errs.ErrUsage)
	}

	return current, nil
}

// hasRealSubcommands ignores the `help` child urfave injects into every
// command at runtime.
func hasRealSubcommands(cmd *cli.Command) bool {
	for _, sub := range cmd.Commands {
		if sub.Name != "help" {
			return true
		}
	}

	return false
}

func findSubcommand(cmd *cli.Command, name string) *cli.Command {
	for _, sub := range cmd.Commands {
		if sub.Name == name {
			return sub
		}
	}

	return nil
}

// parsePlanSets validates every --set pair against the target command's
// flag names, so typos fail loudly at write time.
func parsePlanSets(sets []string, target *cli.Command, scope string) (map[string]any, error) {
	known := map[string]bool{}

	for _, flag := range target.Flags {
		for _, name := range flag.Names() {
			known[name] = true
		}
	}

	values := map[string]any{}

	for _, set := range sets {
		key, value, found := strings.Cut(set, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("plan: --set %q is not flag=value: %w", set, errs.ErrUsage)
		}

		if !known[key] {
			return nil, fmt.Errorf("plan: %q is not a flag of %q (known: %s): %w",
				key, scope, strings.Join(sortedKeys(known), ", "), errs.ErrUsage)
		}

		values[key] = value
	}

	return values, nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func loadPlanForMerge(path string) (map[string]map[string]any, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the caller's own plan file.
	if os.IsNotExist(err) {
		return map[string]map[string]any{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("plan: read %s: %w", path, err)
	}

	var plan map[string]map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("plan: %s is not a {scope: {flag: value}} JSON object: %w: %w", path, err, errs.ErrMalformedInput)
	}

	return plan, nil
}

func writePlanFile(path string, plan map[string]map[string]any) (string, error) {
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("plan: encode: %w", err)
	}

	raw = append(raw, '\n')

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", fmt.Errorf("plan: create %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("plan: write %s: %w", path, err)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:]), nil
}
