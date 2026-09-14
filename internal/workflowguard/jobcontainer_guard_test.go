// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A job container starts BEFORE the first step. Nothing in the workflow can
// validate it, log it, or refuse it: by the time any `run:` executes, the
// caller's image is already the thing executing it. That is why
// lint-megalinter.yml validates its image in a step on the plain runner and
// passes it to `docker run` instead of declaring `container:`.
//
// Every other container job here takes `runtime-image` from its caller, and
// that is a supported override — docs/threat-model.md tells adopters to pin it
// to a digest when they want a stricter posture. The override is not a
// privilege escalation: the caller already holds whatever permissions it grants
// the job.
//
// What the caller may not be able to see is which of those jobs will hand the
// image durable write authority. Most container jobs here hold none: they parse
// a plan, render a summary, build into an artifact. Two do. An adopter pointing
// `runtime-image` at something they have not reviewed should be told that in
// those two the image can write to the repository or the package registry, and
// that no step-level check stands between the two facts.
//
// So this guard does not forbid the pattern. It requires the short list to stay
// short and stay written down.
func declaredWritingContainerJobs() map[string]string {
	return map[string]string{
		"publish-snapshot-npm.yml/build-and-publish": "packages: write",
		"release-create-github.yml/create-release":   "contents: write, id-token: write",
		"version-bump.yml/bump-version":              "contents: write",
	}
}

func TestCallerSelectedContainersWithWriteAuthorityAreDeclared(t *testing.T) {
	t.Parallel()

	workflows := loadLineageWorkflows(t)

	found := map[string]string{}
	callerSelected := 0

	for _, workflow := range workflows {
		for jobName, job := range workflow.jobs {
			image := jobContainerImage(job)
			if image == "" || !expressionPattern.MatchString(image) {
				continue
			}

			callerSelected++

			authority := containerWriteAuthorityOf(mappingChild(job, "permissions"))
			if authority == "" {
				continue
			}

			found[workflow.name+"/"+jobName] = authority
		}
	}

	if callerSelected < 10 {
		t.Fatalf("found %d caller-selected container jobs; the walk, not the tree, is what was measured", callerSelected)
	}

	declared := declaredWritingContainerJobs()

	var findings []string

	for job, authority := range found {
		switch want, ok := declared[job]; {
		case !ok:
			findings = append(findings, fmt.Sprintf(
				"%s starts a caller-selected container and holds %s. The image runs before any step, so nothing "+
					"validates it and nothing stands between it and that authority. Add it to "+
					"declaredWritingContainerJobs and to the runtime-image row in docs/threat-model.md, or move the "+
					"write to a job that does not run a caller-chosen image.", job, authority))
		case want != authority:
			findings = append(findings, fmt.Sprintf("%s now holds %s, declared as %s; re-check the threat-model row", job, authority, want))
		}
	}

	for job := range declared {
		if _, ok := found[job]; !ok {
			findings = append(findings, job+" no longer starts a caller-selected container with write authority; drop the row and the threat-model note")
		}
	}

	sort.Strings(findings)

	if len(findings) > 0 {
		t.Fatalf("caller-selected job containers:\n  %s", strings.Join(findings, "\n  "))
	}
}

// jobContainerImage returns the image a job starts as its container, in either
// spelling: `container: <image>` or `container: {image: <image>}`.
func jobContainerImage(job *yaml.Node) string {
	container := mappingChild(job, "container")
	if container == nil {
		return ""
	}

	if container.Kind == yaml.ScalarNode {
		return container.Value
	}

	return scalarChild(container, "image")
}

// containerWriteAuthorityOf names the write permissions that outlive the job.
// security-events and attestations are deliberately included alongside contents
// and packages: each writes a durable record under the repository's name.
// id-token is included too, because a token minted inside a caller-chosen image
// is a credential that image can use elsewhere.
func containerWriteAuthorityOf(permissions *yaml.Node) string {
	var held []string

	for _, key := range []string{"contents", "packages", "attestations", "id-token", "security-events"} {
		if scalarChild(permissions, key) == "write" {
			held = append(held, key+": write")
		}
	}

	return strings.Join(held, ", ")
}
