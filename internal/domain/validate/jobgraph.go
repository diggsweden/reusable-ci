// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	jobGraphNeedsOutput = regexp.MustCompile(`needs\.([A-Za-z0-9_-]+)\.outputs`)
	jobGraphJobHeader   = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):[ \t]*$`)
	jobGraphReusable    = regexp.MustCompile(`\.ya?ml(@|$)`)
)

// JobGraphViolation is one masking-risk edge, located for annotation.
type JobGraphViolation struct {
	Line int
	Msg  string
}

// CheckJobGraph finds the "reusable consumer masks a skipped producer" hazard.
//
// On GitHub/Gitea/Forgejo, a reusable-workflow-call job (`uses: …/x.yml`) whose
// `if:` (or, when it is `always()`, its `with:`) reads `needs.<P>.outputs.<k>`
// aborts the WHOLE run if `<P>` is skipped — with a misleading "<P> is missing
// the output <k>" that hides the real upstream failure. A producer is treated as
// skippable when it declares an `if:` that is not exactly always()/!cancelled().
//
// A provably-safe edge is acknowledged by a `# job-graph-guard: allow reason=…`
// comment anywhere in the consumer job's block. Pure: caller supplies the YAML
// and owns IO/annotation.
func CheckJobGraph(workflowYAML []byte) ([]JobGraphViolation, error) {
	var doc struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}

	if err := yaml.Unmarshal(workflowYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}

	allowed := jobGraphAllowSet(workflowYAML)

	type jobShape struct {
		Uses string    `yaml:"uses"`
		If   string    `yaml:"if"`
		With yaml.Node `yaml:"with"`
	}

	skippable := make(map[string]bool, len(doc.Jobs))

	for name, node := range doc.Jobs {
		var shape jobShape

		_ = node.Decode(&shape)
		skippable[name] = isSkippableIf(shape.If)
	}

	var violations []JobGraphViolation

	for _, name := range sortedKeys(doc.Jobs) {
		node := doc.Jobs[name]

		var shape jobShape
		if err := node.Decode(&shape); err != nil {
			continue
		}

		if !jobGraphReusable.MatchString(shape.Uses) {
			continue // only reusable-call consumers can mask
		}

		refs := producersIn(shape.If)
		if strings.Contains(shape.If, "always()") {
			withText, _ := yaml.Marshal(&shape.With)
			refs = append(refs, producersIn(string(withText))...)
		}

		violations = appendMaskingViolations(violations, name, node.Line, refs, skippable, allowed)
	}

	return violations, nil
}

// appendMaskingViolations appends one violation per skippable producer that job
// `name` reads from (deduped), unless `name` is in the allow set.
func appendMaskingViolations(violations []JobGraphViolation, name string, line int, refs []string, skippable, allowed map[string]bool) []JobGraphViolation {
	seen := map[string]bool{}

	for _, prod := range refs {
		if seen[prod] || !skippable[prod] || allowed[name] {
			continue
		}

		seen[prod] = true

		violations = append(violations, JobGraphViolation{
			Line: line,
			Msg: fmt.Sprintf("reusable job %q reads needs.%s.outputs, but producer %q is skippable (a non-always() if). A skipped %q makes the run abort with a masked \"missing output\"; make %q run unconditionally (if: always()) and always emit the output, or annotate %q with '# job-graph-guard: allow reason=…' if provably safe.",
				name, prod, prod, prod, prod, name),
		})
	}

	return violations
}

// isSkippableIf reports whether an if-expression can evaluate false (so the job
// may be skipped). always()/!cancelled() never do, and an absent if means the
// job runs when its needs succeed — none of those is a masking source.
func isSkippableIf(ifExpr string) bool {
	stripped := strings.TrimSpace(ifExpr)
	stripped = strings.TrimSuffix(strings.TrimPrefix(stripped, "${{"), "}}")
	stripped = strings.TrimSpace(stripped)

	switch stripped {
	case "", "always()", "!cancelled()", "! cancelled()":
		return false
	default:
		return true
	}
}

func producersIn(text string) []string {
	matches := jobGraphNeedsOutput.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))

	for _, m := range matches {
		out = append(out, m[1])
	}

	return out
}

// jobGraphAllowSet returns the set of jobs carrying a `# job-graph-guard: allow`
// comment anywhere in their block. yaml.v3 comment attachment is unreliable for
// comments between flow keys, so scan by indentation: job headers are at two
// spaces; a deeper line belongs to the current job until the next header.
func jobGraphAllowSet(src []byte) map[string]bool {
	allowed := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	current := ""

	for scanner.Scan() {
		line := scanner.Text()

		if m := jobGraphJobHeader.FindStringSubmatch(line); m != nil {
			current = m[1]

			continue
		}

		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			current = "" // left the jobs/section indentation
		}

		if current != "" && strings.Contains(line, "job-graph-guard: allow") {
			allowed[current] = true
		}
	}

	return allowed
}
