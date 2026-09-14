// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"gopkg.in/yaml.v3"
)

// The predicate type a workflow attests with has two rules behind it, and the
// old guard checked neither.
//
// It searched every workflow for the regex `\bslsaprovenance[0-9]*\b` and
// complained about any match that was not exactly "slsaprovenance1". That fails
// open in the direction that matters: `--type spdx` does not match the pattern
// at all, so it was never looked at. It also stated something untrue in its own
// failure message — that the CLI accepts only slsaprovenance1 — when the CLI
// accepts cyclonedx, spdx and predicate-type URIs as well.
//
// The real rules, from internal/app/container/attest.go:
//
//  1. "slsaprovenance" (bare) is SLSA v0.2 and is rejected outright, because the
//     predicate the engine emits is v1.0 and labelling it v0.2 would mislead
//     verifiers.
//  2. A predicate is generated from the CI environment ONLY for slsaprovenance1.
//     Every other type needs --predicate, and without one the run fails.
//
// Rule 2 is the one with teeth for adopters: a workflow that exposes the
// predicate type as an input, and passes no --predicate, has published a knob
// where every setting but one fails at run time.

type attestInvocation struct {
	Workflow  string
	Job       string
	Step      string
	Type      string // resolved as far as the workflow states it
	FromEnv   string // env var the type came from, when it did
	Input     string // workflow_call input the type came from, when it did
	Literal   bool   // the type is a literal in argv or an env value
	Predicate bool   // --predicate is passed
}

func TestAttestInvocationsUseAPredicateTypeThatCanWork(t *testing.T) {
	t.Parallel()

	invocations := collectAttestInvocations(t)
	if len(invocations) < 3 {
		t.Fatalf("found %d container attest invocations; the walk, not the workflows, is what was measured", len(invocations))
	}

	var findings []string

	for _, in := range invocations {
		where := fmt.Sprintf("%s job %s step %q", in.Workflow, in.Job, in.Step)

		// A caller-supplied type is checked against the input's declared
		// contract instead, by the test below.
		if in.Input != "" {
			continue
		}

		switch {
		case in.Type == "":
			findings = append(findings, where+": --type is not stated; it cannot be checked and the CLI refuses an empty one")
		case in.Type == "slsaprovenance":
			findings = append(findings, where+": --type slsaprovenance is SLSA v0.2 and is rejected; use slsaprovenance1")
		case !in.Predicate && in.Type != domaincontainer.PredicateTypeSLSAProvenance1:
			findings = append(findings, fmt.Sprintf(
				"%s: --type %s with no --predicate; only %s generates a predicate from the CI environment",
				where, in.Type, domaincontainer.PredicateTypeSLSAProvenance1))
		}
	}

	sort.Strings(findings)

	if len(findings) > 0 {
		t.Fatalf("attestation predicate-type contract:\n  %s", strings.Join(findings, "\n  "))
	}
}

// TestAttestPredicateTypeInputsDoNotAdvertiseValuesThatFail is rule 2 stated as
// a contract for adopters rather than as a runtime surprise.
//
// slsa-attestor.yml takes `predicate-type` from its caller and passes no
// --predicate. Its default works; every other value the description advertised
// does not. An input's description is what an adopter reads before choosing, so
// advertising a value that cannot work there is the bug, not a documentation
// nicety.
func TestAttestPredicateTypeInputsDoNotAdvertiseValuesThatFail(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, in := range collectAttestInvocations(t) {
		if in.Input == "" || in.Predicate {
			continue
		}

		checked++

		spec := workflowCallInput(t, in.Workflow, in.Input)
		if spec == nil {
			t.Errorf("%s: --type comes from input %q, which the workflow does not declare", in.Workflow, in.Input)

			continue
		}

		if got := scalarChild(spec, "default"); got != domaincontainer.PredicateTypeSLSAProvenance1 {
			t.Errorf("%s input %s: default %q would fail without --predicate; want %s",
				in.Workflow, in.Input, got, domaincontainer.PredicateTypeSLSAProvenance1)
		}

		description := scalarChild(spec, "description")
		for _, advertised := range []string{"<uri>", "cyclonedx", "spdx"} {
			if strings.Contains(description, advertised) {
				t.Errorf("%s input %s advertises %q, but this workflow passes no --predicate so only %s works: %q",
					in.Workflow, in.Input, advertised, domaincontainer.PredicateTypeSLSAProvenance1, description)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no attest invocation takes its predicate type from an input; this check measured nothing")
	}
}

// collectAttestInvocations finds every `reusable-ci container attest` run step
// in every workflow and resolves its --type as far as the workflow states it:
// a literal in argv, an env value, or a workflow_call input behind one.
func collectAttestInvocations(t *testing.T) []attestInvocation {
	t.Helper()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var found []attestInvocation

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // repository workflow fixture.
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}

		var doc yaml.Node
		if unmarshalErr := yaml.Unmarshal(body, &doc); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), unmarshalErr)
		}

		found = append(found, attestInvocationsIn(entry.Name(), doc.Content[0])...)
	}

	return found
}

func attestInvocationsIn(workflow string, root *yaml.Node) []attestInvocation {
	jobs := mappingChild(root, "jobs")
	if jobs == nil {
		return nil
	}

	var found []attestInvocation

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		jobName, job := jobs.Content[i].Value, jobs.Content[i+1]

		steps := mappingChild(job, "steps")
		if steps == nil {
			continue
		}

		for _, step := range steps.Content {
			run := scalarChild(step, "run")
			if !strings.Contains(run, "reusable-ci container attest") &&
				!strings.Contains(run, "container attest \\") {
				continue
			}

			in := attestInvocation{Workflow: workflow, Job: jobName, Step: scalarChild(step, "name")}
			in.Type, in.FromEnv, in.Literal = attestTypeArgument(run, mappingChild(step, "env"))
			in.Input = workflowInputExpression(in.FromEnv, mappingChild(step, "env"))
			in.Predicate = predicateFlagPattern.MatchString(run)

			found = append(found, in)
		}
	}

	return found
}

var (
	// The argument may be a bare word or a shell variable, quoted or not.
	typeFlagPattern      = regexp.MustCompile(`--type[= ]+"?\$?\{?([A-Za-z0-9_./:-]+)\}?"?`)
	predicateFlagPattern = regexp.MustCompile(`--predicate[= ]`)
	inputExprPattern     = regexp.MustCompile(`\$\{\{\s*inputs(?:\.|\['|\[")([A-Za-z0-9_-]+)('\]|"\])?\s*\}\}`)
)

// attestTypeArgument reads --type out of the run block. When the argument is a
// shell variable it is resolved through the step's own env mapping, which is
// where these workflows put it.
func attestTypeArgument(run string, env *yaml.Node) (string, string, bool) {
	// Line continuations first: the argument is usually on its own line.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(run, "\\\n", " ")), " ")

	match := typeFlagPattern.FindStringSubmatch(flat)
	if match == nil {
		return "", "", false
	}

	argument := match[1]

	// A bare word is the type itself unless the run block spelled it as a
	// variable, which the pattern captured without its $ or braces.
	if !strings.Contains(flat, "$"+argument) && !strings.Contains(flat, "${"+argument+"}") {
		return argument, "", true
	}

	value := scalarChild(env, argument)
	if value == "" {
		return "", argument, false
	}

	if inputExprPattern.MatchString(value) {
		return "", argument, false
	}

	return value, argument, true
}

// workflowInputExpression returns the workflow_call input name behind an env
// value like `${{ inputs.predicate-type }}`.
func workflowInputExpression(envVar string, env *yaml.Node) string {
	if envVar == "" {
		return ""
	}

	match := inputExprPattern.FindStringSubmatch(scalarChild(env, envVar))
	if match == nil {
		return ""
	}

	return match[1]
}

func workflowCallInput(t *testing.T, workflow, input string) *yaml.Node {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", workflow)) //nolint:gosec // repository workflow fixture.
	if err != nil {
		t.Fatal(err)
	}

	var doc yaml.Node
	if unmarshalErr := yaml.Unmarshal(body, &doc); unmarshalErr != nil {
		t.Fatalf("parse %s: %v", workflow, unmarshalErr)
	}

	return mappingChild(mappingChild(mappingChild(mappingChild(doc.Content[0], "on"), "workflow_call"), "inputs"), input)
}
