// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	jobGraphReusable = regexp.MustCompile(`\.ya?ml(@|$)`)

	// The annotation, and the reason that makes it count. They are separate
	// patterns so an annotation WITHOUT a usable reason can be reported as
	// such: silently not applying it would leave the operator reading the
	// generic masking error while looking straight at the override they
	// thought they had written.
	//
	// A reason may be quoted or bare; what it may not be is empty. The point
	// of the annotation is the record of WHY an edge is provably safe, and a
	// bare `allow` records nothing.
	jobGraphGuardAllow  = regexp.MustCompile(`#\s*job-graph-guard:\s*allow\b`)
	jobGraphGuardReason = regexp.MustCompile(`\breason\s*=\s*(?:"\s*([^"]*?)\s*"|'\s*([^']*?)\s*'|(\S+))`)
)

// guardReasonGiven reports whether line carries a non-empty reason= value.
func guardReasonGiven(line string) bool {
	m := jobGraphGuardReason.FindStringSubmatch(line)
	if m == nil {
		return false
	}

	for _, group := range m[1:] {
		if strings.TrimSpace(group) != "" {
			return true
		}
	}

	return false
}

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
func CheckJobGraph(workflowYAML []byte) ([]JobGraphViolation, error) { //nolint:cyclop // jobs, effective scalar input references and exemptions have separate refusal paths.
	var doc struct {
		Jobs yaml.Node `yaml:"jobs"`
	}

	if err := yaml.Unmarshal(workflowYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}

	jobs := map[string]yaml.Node{}
	if doc.Jobs.Kind != 0 {
		if err := doc.Jobs.Decode(&jobs); err != nil {
			return nil, fmt.Errorf("parse workflow jobs: %w", err)
		}
	}

	allowed, unreasoned := jobGraphAllowSet(workflowYAML, &doc.Jobs)

	type jobShape struct {
		Uses string    `yaml:"uses"`
		If   string    `yaml:"if"`
		With yaml.Node `yaml:"with"`
	}

	skippable := make(map[string]bool, len(jobs))

	for name, node := range jobs {
		var shape jobShape

		_ = node.Decode(&shape)
		skippable[name] = isSkippableIf(shape.If)
	}

	var violations []JobGraphViolation

	for _, name := range sortedKeys(jobs) {
		node := jobs[name]

		var shape jobShape
		if err := node.Decode(&shape); err != nil {
			continue
		}

		if !jobGraphReusable.MatchString(shape.Uses) {
			continue // only reusable-call consumers can mask
		}

		refs := producersIn(shape.If, true)
		if strings.Contains(shape.If, "always()") {
			pending := []*yaml.Node{&shape.With}
			seen := map[*yaml.Node]bool{}

			for len(pending) > 0 {
				node := resolveAlias(pending[len(pending)-1])
				pending = pending[:len(pending)-1]

				if node == nil || seen[node] {
					continue
				}

				seen[node] = true
				if node.Kind == yaml.ScalarNode {
					refs = append(refs, producersIn(node.Value, false)...)
				}

				for index := len(node.Content) - 1; index >= 0; index-- {
					pending = append(pending, node.Content[index])
				}
			}
		}

		// An override that records no reason is reported in its own right,
		// rather than being dropped so the job falls back to the generic
		// masking error. The operator wrote an annotation; tell them why it
		// did not take effect.
		if unreasoned[name] {
			violations = append(violations, JobGraphViolation{
				Line: node.Line,
				Msg: fmt.Sprintf("job %q carries '# job-graph-guard: allow' with no reason; the annotation waives a masking check, "+
					"so it must record WHY the edge is provably safe — write '# job-graph-guard: allow reason=\"…\"'", name),
			})
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

func producersIn(text string, bare bool) []string {
	var out []string

	for _, ref := range expressionReferences(text, "needs", bare) {
		if len(ref.members) >= 2 && ref.members[1] == "outputs" {
			out = append(out, ref.members[0])
		}
	}

	return out
}

// jobGraphAllowSet returns the set of jobs carrying a `# job-graph-guard: allow`
// comment anywhere in their block, and the set whose annotation recorded no
// reason. yaml.v3 does not attach comments reliably, so the source is scanned
// line by line; which job a line belongs to comes from the parsed job headers
// (their line and column), not from an assumed indentation. A job's block runs
// from its own header line, trailing comment included, to the next header; a
// non-comment line indented less than the headers ends the jobs mapping.
func jobGraphAllowSet(src []byte, jobs *yaml.Node) (map[string]bool, map[string]bool) {
	allowed := map[string]bool{}
	unreasoned := map[string]bool{}

	headers := jobHeaders(jobs)

	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	current, column, lineNo := "", 0, 0

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		for len(headers) > 0 && headers[0].line <= lineNo {
			current, column = headers[0].name, headers[0].column
			headers = headers[1:]
		}

		if current != "" && shallowerThan(line, column) {
			current = "" // a shallower key: the jobs mapping has ended
		}

		if current == "" || !jobGraphGuardAllow.MatchString(line) {
			continue
		}

		if guardReasonGiven(line) {
			allowed[current] = true
		} else {
			unreasoned[current] = true
		}
	}

	// An annotation with a reason wins over one without, so a job carrying
	// both spellings is allowed rather than reported.
	for name := range allowed {
		delete(unreasoned, name)
	}

	return allowed, unreasoned
}

// jobHeader is one job's key in the jobs mapping: its name and where the
// key sits, so the source lines below it can be attributed to that job.
type jobHeader struct {
	name         string
	line, column int
}

func jobHeaders(jobs *yaml.Node) []jobHeader {
	var headers []jobHeader

	for index := 0; jobs != nil && index+1 < len(jobs.Content); index += 2 {
		key := jobs.Content[index]
		headers = append(headers, jobHeader{name: key.Value, line: key.Line, column: key.Column})
	}

	sort.Slice(headers, func(i, j int) bool { return headers[i].line < headers[j].line })

	return headers
}

// shallowerThan reports whether a non-blank, non-comment line is indented
// less than the job key at column, which ends that job's block.
func shallowerThan(line string, column int) bool {
	trimmed := strings.TrimLeft(line, " \t")

	return trimmed != "" && !strings.HasPrefix(trimmed, "#") && len(line)-len(trimmed)+1 < column
}
