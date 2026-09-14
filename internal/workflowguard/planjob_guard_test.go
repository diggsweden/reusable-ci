// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

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
//   - a `plan write` invocation is the plan author (a), not a blanket exemption;
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
	jobVars := binaryVariables(job)

	for idx, step := range steps.Content {
		invocations := binaryCalls(scalarChild(step, "run"), mergeBinaryVars(jobVars, binaryVariables(step)))
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

	// A plan write accounts for that invocation only, not unrelated calls in
	// the same step. It has no plan-scoped flags, so it drops out naturally.
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

	slices.Sort(paths)

	for _, path := range paths {
		if locs := readers[path]; len(locs) > 1 {
			audit.violations = append(audit.violations, fmt.Sprintf(
				"%d steps invoke plan-scoped `reusable-ci %s` with no step-level %s override; "+
					"the job plan feeds ALL of them — keep one designated reader:\n    %s",
				len(locs), path, planEnvKey, strings.Join(locs, "\n    ")))
		}
	}
}

// binaryName is the executable every plan-scoped invocation ends up running,
// however the step spells the path to it.
const binaryName = "reusable-ci"

// isCommandPrefix reports whether a token runs another command and is therefore
// transparent to this accounting: what follows is still the invocation whose
// plan sources matter.
func isCommandPrefix(token string) bool {
	switch token {
	case "env", "command", "exec", "sudo", "time":
		return true
	default:
		return false
	}
}

// binaryCalls extracts every reusable-ci invocation from a run block as the
// token tail following the executable. Comments are dropped from the first
// unquoted word-initial "#" to end of line, so a trailing comment can neither
// hide a real call nor invent one out of prose. Backslash line continuations
// are joined so multi-line invocations parse whole.
//
// The executable is recognized by any spelling that resolves to the same
// binary: bare, quoted, path-qualified, reached through a variable whose value
// this workflow sets to such a path, or preceded by env/command/exec/sudo and
// leading NAME=value assignments. Missing one of those spellings is what makes
// a guard fail open, which is worse than a guard that is absent.
//
// This is tokenization, not a shell parser: pipelines, substitutions, arrays
// and conditionals are not modeled, and a caller that builds the command at
// runtime is outside the claim.
func binaryCalls(run string, binaryVars map[string]bool) [][]string {
	if run == "" {
		return nil
	}

	kept := make([]string, 0, 8)

	for _, line := range strings.Split(run, "\n") {
		kept = append(kept, stripShellComment(line))
	}

	var calls [][]string
	for _, line := range strings.Split(strings.ReplaceAll(strings.Join(kept, "\n"), "\\\n", " "), "\n") {
		calls = append(calls, lineCalls(strings.Fields(line), binaryVars)...)
	}

	return calls
}

// isSeparator reports whether a token ends one command, putting the next token
// back in command position.
func isSeparator(token string) bool {
	switch token {
	case "&&", "||", "|", ";", "{", "(", "!":
		return true
	default:
		return false
	}
}

// lineCalls records the invocations of one logical line. A binary token only
// counts in command position: the same path as an argument to install, hash or
// copy is a file being handled, not a command being run.
func lineCalls(tokens []string, binaryVars map[string]bool) [][]string {
	var calls [][]string

	command := true

	for index, token := range tokens {
		switch {
		case isSeparator(token):
			command = true
		case command && (isCommandPrefix(token) || isAssignment(token)):
			// Transparent: the executable is still to come.
		case command && isBinaryToken(token, binaryVars):
			calls = append(calls, tokens[index+1:])
			command = false
		default:
			command = false
		}
	}

	return calls
}

// isAssignment reports whether a token is a leading NAME=value environment
// assignment rather than an option or an argument. A value that opens a command
// substitution is not transparent: the command being run is the one inside it,
// and substitutions are outside this tokenizer's model.
func isAssignment(token string) bool {
	name, value, ok := strings.Cut(token, "=")
	if !ok || name == "" || strings.HasPrefix(token, "-") {
		return false
	}

	if strings.Contains(value, "$(") || strings.Contains(value, "`") {
		return false
	}

	for _, char := range name {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_' {
			return false
		}
	}

	return true
}

// stripShellComment removes a word-initial unquoted "#" and everything after
// it. A "#" inside a word (a fragment identifier, a colour literal) or inside
// quotes is data, not a comment.
func stripShellComment(line string) string {
	quote := byte(0)

	for i := range len(line) {
		char := line[i]
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}

	return line
}

// isBinaryToken reports whether one token names the reusable-ci executable:
// a bare or quoted name, a path ending in it, or a variable reference this
// workflow assigns such a path to.
func isBinaryToken(token string, binaryVars map[string]bool) bool {
	token = strings.Trim(token, `"'`)
	if name, ok := variableName(token); ok {
		return binaryVars[name]
	}

	return token == binaryName || strings.HasSuffix(token, "/"+binaryName)
}

// variableName returns the name behind $VAR or ${VAR}.
func variableName(token string) (string, bool) {
	rest, ok := strings.CutPrefix(token, "$")
	if !ok {
		return "", false
	}

	if inner, braced := strings.CutPrefix(rest, "{"); braced {
		name, closed := strings.CutSuffix(inner, "}")

		return name, closed
	}

	return rest, rest != ""
}

func mergeBinaryVars(sets ...map[string]bool) map[string]bool {
	merged := map[string]bool{}

	for _, set := range sets {
		for name := range set {
			merged[name] = true
		}
	}

	return merged
}

// binaryVariables collects the variables one workflow, job and step set to a
// path whose last element is the reusable-ci executable, so "$RC_BIN foo" is
// accounted as the invocation it is.
func binaryVariables(nodes ...*yaml.Node) map[string]bool {
	vars := map[string]bool{}

	for _, node := range nodes {
		env := mappingChild(node, "env")
		if env == nil || env.Kind != yaml.MappingNode {
			continue
		}

		for i := 0; i+1 < len(env.Content); i += 2 {
			value := env.Content[i+1]
			if value.Kind != yaml.ScalarNode {
				continue
			}

			if trimmed := strings.Trim(value.Value, `"'`); trimmed == binaryName || strings.HasSuffix(trimmed, "/"+binaryName) {
				vars[env.Content[i].Value] = true
			}
		}
	}

	return vars
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

// TestBinaryCallDiscoverySpellingsAndComments pins what counts as an
// invocation. The accounting above is only worth having if it sees the calls:
// a spelling it misses is a plan reader that never gets counted, which is the
// silent-poison case the guard exists to catch, reappearing as a guard that
// passes. The converse matters too — a path being installed, copied or hashed
// is a file, not a command, and counting it would turn the guard into noise.
func TestBinaryCallDiscoverySpellingsAndComments(t *testing.T) {
	t.Parallel()

	vars := map[string]bool{"RC_BIN": true}

	for _, testCase := range []struct {
		name string
		run  string
		want [][]string
	}{
		{"bare", "reusable-ci container build", [][]string{{"container", "build"}}},
		{"absolute path", "/usr/local/bin/reusable-ci container build", [][]string{{"container", "build"}}},
		{"relative path", "./reusable-ci container build", [][]string{{"container", "build"}}},
		{"quoted", `"reusable-ci" container build`, [][]string{{"container", "build"}}},
		{"variable", `"$RC_BIN" security report enrich-sarif`, [][]string{{"security", "report", "enrich-sarif"}}},
		{"braced variable", "${RC_BIN} security report enrich-sarif", [][]string{{"security", "report", "enrich-sarif"}}},
		{"unknown variable", `"$OTHER_BIN" container build`, nil},
		{"env prefix", "env FOO=1 reusable-ci container build", [][]string{{"container", "build"}}},
		{"leading assignments", `GITHUB_PATH="" REUSABLE_CI_INSTALL_DIR=/tmp reusable-ci container build`, [][]string{{"container", "build"}}},
		{"exec prefix", "exec reusable-ci container build", [][]string{{"container", "build"}}},
		{"sudo prefix", "sudo reusable-ci container build", [][]string{{"container", "build"}}},
		{"installed, not run", "sudo install -m0755 cli-prebuilt/reusable-ci /usr/local/bin/reusable-ci", nil},
		{"extracted, not run", `tar -xzf "$tarball" -C cli-prebuilt reusable-ci`, nil},
		{"hashed, not run", `want="$(sha256sum cli-prebuilt/reusable-ci | awk '{print $1}')"`, nil},
		{"copied, not run", `docker cp "$cid:/usr/local/bin/reusable-ci" /tmp/baked-reusable-ci`, nil},
		{"comment line", "# reusable-ci container build is pre-baked", nil},
		{"trailing comment", "reusable-ci container build # and not reusable-ci plan write", [][]string{{"container", "build"}}},
		{"hash inside a word", "reusable-ci container build --label a#b", [][]string{{"container", "build", "--label", "a#b"}}},
		{"hash inside quotes", `reusable-ci container build --label "a # b"`, [][]string{{"container", "build", "--label", `"a`, "#", `b"`}}},
		{"two commands on one line", "reusable-ci plan write && reusable-ci container build", [][]string{{"plan", "write", "&&", "reusable-ci", "container", "build"}, {"container", "build"}}},
		{"piped", "reusable-ci plan show | reusable-ci container build", [][]string{{"plan", "show", "|", "reusable-ci", "container", "build"}, {"container", "build"}}},
		{"line continuation", "reusable-ci container \\\n  build --push", [][]string{{"container", "build", "--push"}}},
		{"separate lines", "reusable-ci plan write\nreusable-ci container build", [][]string{{"plan", "write"}, {"container", "build"}}},
		{"empty", "", nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, binaryCalls(testCase.run, vars))
		})
	}
}

// TestBinaryVariablesFollowWorkflowEnv proves the variable spelling is only
// honoured for a name this workflow actually points at the binary, so an
// unrelated variable cannot be read as an invocation.
func TestBinaryVariablesFollowWorkflowEnv(t *testing.T) {
	t.Parallel()

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`
env:
  RC_BIN: /tmp/nl-bin/reusable-ci
  BARE: reusable-ci
  SARIF_FILE: /tmp/nl-src/report.sarif
  NEARLY: /tmp/reusable-ci-plan.json
`), &doc))

	require.Equal(t, map[string]bool{"RC_BIN": true, "BARE": true}, binaryVariables(doc.Content[0]))
	require.Empty(t, binaryVariables(nil))
}

// TestJobLevelPlanGuardCatchesVariableSpelledReaders is the fail-open case:
// two steps read the same plan-scoped command through a variable the job env
// points at the binary. Before the spellings above were recognized, neither
// call was counted and the guard reported a clean job.
func TestJobLevelPlanGuardCatchesVariableSpelledReaders(t *testing.T) {
	t.Parallel()

	tree := cli.New(cli.BuildInfo{Version: "test"})
	scoped := planScopedCommandPaths(t, tree)

	const body = "jobs:\n  build:\n    env:\n      REUSABLE_CI_PLAN: .plan.json\n      RC_BIN: /tmp/bin/reusable-ci\n    steps:\n" +
		"      - run: '\"$RC_BIN\" container build'\n" +
		"      - run: sudo /usr/local/bin/reusable-ci container build\n"

	violations := auditFixture(t, tree, scoped, body)
	require.Len(t, violations, 1)
	require.Contains(t, violations[0], "container build")
}
