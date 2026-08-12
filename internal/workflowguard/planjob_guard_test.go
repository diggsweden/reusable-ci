// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/cliflags"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
	urfavecli "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// planEnvKey mirrors planfile.EnvVar; kept literal here because the
// test greps workflow YAML for the exact string a job author writes.
const planEnvKey = "REUSABLE_CI_PLAN"

// TestJobLevelPlanAccountsForEveryBinaryCall audits every workflow job
// that sets REUSABLE_CI_PLAN in its job-level env. Such an env makes
// the plan visible to EVERY `reusable-ci` invocation in the job, and
// for any plan-scoped command (one whose flags carry plan-file sources,
// derived below from the real command tree — see
// TestPlanScopesMatchCommandPaths for the scope↔path pinning) the plan
// silently outranks that step's env block (flag > plan > env).
//
// The near-miss this guards: publish-container.yml's build-arch job
// runs `container build` twice — the real build (the plan's designated
// reader) and a binary-extraction build that MUST NOT read the plan and
// therefore blank-overrides REUSABLE_CI_PLAN at step level. Drop that
// override and the extraction build is silently poisoned with the main
// build's push mode, cache, and scan settings.
//
// Accounting rules per step that invokes `reusable-ci`:
//   - the step also invokes `plan write` → it is the plan author (a);
//   - the step's own env sets REUSABLE_CI_PLAN (any value, blank
//     included) → deliberate override (b);
//   - the invoked command is not plan-scoped → it has no plan sources,
//     the plan cannot feed it (c);
//   - otherwise the step READS the job plan: at most one such step per
//     plan-scoped command per job may exist. A second one is exactly
//     the silent-poison case and fails here.
func TestJobLevelPlanAccountsForEveryBinaryCall(t *testing.T) {
	t.Parallel()

	tree := cli.New(cli.BuildInfo{Version: "dev"})

	scoped := planScopedCommandPaths(t, tree)
	require.NotEmpty(t, scoped,
		"no plan-scoped commands derived from the CLI tree; the plan-source reflection hook broke")

	audit := auditPlanJobs(t, filepath.Join(reporoot.Path(t), ".github", "workflows"), tree, scoped)

	// The known plan job must have been walked — if the walker stops
	// seeing it, the guard is dead weight, not green.
	require.Containsf(t, audit.jobs, "publish-container.yml jobs.build-arch",
		"expected plan job not found; audited: %v", audit.jobs)

	require.Emptyf(t, audit.violations,
		"job-level %s plans feed every plan-scoped `reusable-ci` call in the job; "+
			"unaccounted calls found:\n  %s\nfix by blank-overriding %s: \"\" in the step's env "+
			"(the deliberate opt-out) or by making the step the job's single designated plan reader",
		planEnvKey, strings.Join(audit.violations, "\n  "), planEnvKey)
}

// TestJobLevelPlanGuardCatchesPoison proves the audit fails when it
// should, against fixtures: a second non-overridden `container build`
// in a plan job (the exact publish-container near-miss), an
// unresolvable invocation, and the blank-override escape hatch.
func TestJobLevelPlanGuardCatchesPoison(t *testing.T) {
	t.Parallel()

	tree := cli.New(cli.BuildInfo{Version: "dev"})
	scoped := planScopedCommandPaths(t, tree)

	const header = "name: fixture\non: push\njobs:\n  build-arch:\n    runs-on: ubuntu-24.04\n" +
		"    env:\n      " + planEnvKey + ": /tmp/plan.json\n    steps:\n" +
		"      - name: Write build plan\n" +
		"        run: reusable-ci plan write --scope \"container build\" --set \"context=.\"\n" +
		"      - name: Build\n        run: reusable-ci container build\n"

	t.Run("second reader without override is flagged", func(t *testing.T) {
		t.Parallel()

		violations := auditFixture(t, tree, scoped, header+
			"      - name: Extract binaries\n        run: reusable-ci container build\n")

		require.Len(t, violations, 1)
		require.Contains(t, violations[0], "container build")
		require.Contains(t, violations[0], "Extract binaries")
	})

	t.Run("unresolvable invocation is flagged", func(t *testing.T) {
		t.Parallel()

		violations := auditFixture(t, tree, scoped, header+
			"      - name: Typo\n        run: reusable-ci frobnicate\n")

		require.Len(t, violations, 1)
		require.Contains(t, violations[0], "cannot resolve")
	})

	t.Run("blank step-level override passes", func(t *testing.T) {
		t.Parallel()

		violations := auditFixture(t, tree, scoped, header+
			"      - name: Extract binaries\n        env:\n          "+planEnvKey+": \"\"\n"+
			"        run: reusable-ci container build\n")

		require.Empty(t, violations)
	})
}

// auditFixture writes one workflow fixture into a temp dir and returns
// the audit violations for it.
func auditFixture(t *testing.T, tree *urfavecli.Command, scoped map[string]bool, workflow string) []string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.yml"), []byte(workflow), 0o600))

	audit := auditPlanJobs(t, dir, tree, scoped)
	require.Len(t, audit.jobs, 1, "fixture job not detected as a plan job")

	return audit.violations
}

// planAudit is the outcome of walking workflows for plan jobs.
type planAudit struct {
	jobs       []string // "file.yml jobs.<id>" for every plan job seen
	violations []string
}

// planScopedCommandPaths walks the assembled command tree and returns
// every command path owning at least one flag with a plan-file source
// (the sources expose Scope/PlanKey). Only these commands can read a
// plan; everything else ignores $REUSABLE_CI_PLAN entirely.
func planScopedCommandPaths(t *testing.T, root *urfavecli.Command) map[string]bool {
	t.Helper()

	paths := make(map[string]bool)

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, flag := range cmd.Flags {
			for _, src := range cliflags.Sources(t, flag).Chain {
				if _, ok := src.(interface {
					Scope() string
					PlanKey() string
				}); ok {
					paths[path] = true
				}
			}
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}

	for _, sub := range root.Commands {
		walk(sub.Name, sub)
	}

	return paths
}

// auditPlanJobs parses every workflow in dir and audits each job whose
// job-level env sets a non-blank REUSABLE_CI_PLAN.
func auditPlanJobs(t *testing.T, dir string, tree *urfavecli.Command, scoped map[string]bool) planAudit {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var audit planAudit

	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // walked under the workflows dir.
		require.NoErrorf(t, err, "read %s", entry.Name())

		auditWorkflow(t, entry.Name(), body, tree, scoped, &audit)
	}

	return audit
}

// auditWorkflow finds the plan jobs of one workflow document and audits
// each; non-plan jobs are out of scope by definition (no job-level
// plan env means env blocks keep their normal precedence).
func auditWorkflow(t *testing.T, fileName string, body []byte, tree *urfavecli.Command,
	scoped map[string]bool, audit *planAudit,
) {
	t.Helper()

	var doc yaml.Node

	require.NoErrorf(t, yaml.Unmarshal(body, &doc), "parse %s", fileName)

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return
	}

	jobs := mappingChild(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i < len(jobs.Content); i += 2 {
		jobID, job := jobs.Content[i].Value, jobs.Content[i+1]

		// A blank job-level value disables the plan (planfile.Lookup
		// treats "" as no plan), so only non-blank values arm the audit.
		if scalarChild(mappingChild(job, "env"), planEnvKey) == "" {
			continue
		}

		loc := fileName + " jobs." + jobID
		audit.jobs = append(audit.jobs, loc)
		auditPlanJob(loc, job, tree, scoped, audit)
	}
}

// auditPlanJob applies the accounting rules to every step of one plan
// job and appends violations for unresolvable invocations and for
// multiple non-overridden readers of the same plan-scoped command.
func auditPlanJob(loc string, job *yaml.Node, tree *urfavecli.Command,
	scoped map[string]bool, audit *planAudit,
) {
	steps := mappingChild(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return
	}

	readers := make(map[string][]string) // plan-scoped command path → step locations reading the plan

	for idx, step := range steps.Content {
		invocations := binaryCalls(scalarChild(step, "run"))
		if len(invocations) == 0 {
			continue
		}

		// Rule (b): a step-level REUSABLE_CI_PLAN entry (blank or not)
		// is the deliberate, reviewable opt-out/redirect.
		if mappingChild(mappingChild(step, "env"), planEnvKey) != nil {
			continue
		}

		stepLoc := fmt.Sprintf("%s step %q", loc, stepName(step, idx))
		classifyInvocations(invocations, tree, scoped, stepLoc, readers, audit)
	}

	flagMultipleReaders(readers, audit)
}

// classifyInvocations resolves each `reusable-ci` call of one step
// against the command tree and records plan readers; the plan-write
// step (rule a) and non-plan-scoped commands (rule c) are accounted.
func classifyInvocations(invocations [][]string, tree *urfavecli.Command,
	scoped map[string]bool, stepLoc string, readers map[string][]string, audit *planAudit,
) {
	paths := make([]string, 0, len(invocations))

	for _, tail := range invocations {
		path, ok := resolveCommandPath(tree, tail)
		if !ok {
			audit.violations = append(audit.violations, fmt.Sprintf(
				"%s: cannot resolve `reusable-ci %s` against the command tree",
				stepLoc, strings.Join(head(tail, 3), " ")))

			continue
		}

		paths = append(paths, path)
	}

	// Rule (a): the step authoring the plan is plan-handling by design.
	for _, path := range paths {
		if path == "plan write" {
			return
		}
	}

	for _, path := range paths {
		if scoped[path] { // rule (c) filters the rest out
			readers[path] = append(readers[path], stepLoc)
		}
	}
}

// flagMultipleReaders turns >1 non-overridden readers of one plan-scoped
// command into a violation — the second reader is silently poisoned.
func flagMultipleReaders(readers map[string][]string, audit *planAudit) {
	paths := make([]string, 0, len(readers))
	for path := range readers {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	for _, path := range paths {
		if locs := readers[path]; len(locs) > 1 {
			audit.violations = append(audit.violations, fmt.Sprintf(
				"%d steps invoke plan-scoped `reusable-ci %s` with no step-level %s override; "+
					"the job plan feeds ALL of them — keep one designated reader:\n    %s",
				len(locs), path, planEnvKey, strings.Join(locs, "\n    ")))
		}
	}
}

// binaryCalls extracts every `reusable-ci` invocation from a run block
// as the token tail following the binary name. Shell comment lines are
// dropped (they legitimately mention the binary) and backslash line
// continuations are joined so multi-line invocations parse whole.
func binaryCalls(run string) [][]string {
	if run == "" {
		return nil
	}

	kept := make([]string, 0, 8)

	for _, line := range strings.Split(run, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		kept = append(kept, line)
	}

	text := strings.ReplaceAll(strings.Join(kept, "\n"), "\\\n", " ")
	tokens := strings.Fields(text)

	var calls [][]string

	for i, tok := range tokens {
		if tok == "reusable-ci" {
			calls = append(calls, tokens[i+1:])
		}
	}

	return calls
}

// resolveCommandPath greedily matches the leading tokens of one
// invocation tail down the command tree and returns the canonical
// space-joined command path.
func resolveCommandPath(root *urfavecli.Command, tokens []string) (string, bool) {
	cmd := root

	var parts []string

	for _, tok := range tokens {
		next := subcommandNamed(cmd, tok)
		if next == nil {
			break
		}

		parts = append(parts, next.Name)
		cmd = next
	}

	if len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, " "), true
}

// subcommandNamed returns cmd's direct subcommand matching name (or an
// alias of it), or nil.
func subcommandNamed(cmd *urfavecli.Command, name string) *urfavecli.Command {
	for _, sub := range cmd.Commands {
		if slices.Contains(sub.Names(), name) {
			return sub
		}
	}

	return nil
}

// stepName labels a step by its `name:` or, failing that, its 1-based
// position in the job.
func stepName(step *yaml.Node, idx int) string {
	if name := scalarChild(step, "name"); name != "" {
		return name
	}

	return fmt.Sprintf("#%d", idx+1)
}

// head returns at most n leading elements of tokens, for terse error
// messages about unresolvable invocations.
func head(tokens []string, n int) []string {
	if len(tokens) <= n {
		return tokens
	}

	return tokens[:n]
}
