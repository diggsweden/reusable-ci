// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// TestPublishStageTargets_HaveMatchingJobs proves that every publish-stage
// target key has a job in release-publish-stage.yml whose id maps back to
// it, and that summarize-publish-stage `needs:` every one of them.
//
// This closes a silent failure mode. `report stage-result` maps job id →
// target key through lookupNeeds' dash↔underscore fallback
// (app/summary/stageresult.go). If a new target key ships without a
// matching job id — or with a job the summarize step doesn't need — the
// lookup simply misses, and the target is quietly reported as not having
// run instead of failing loudly. No other guardrail covers this
// direction: workflowcontract_test.go checks `with:`/`inputs:`
// agreement, not target-key/job-id agreement.
func TestPublishStageTargets_HaveMatchingJobs(t *testing.T) {
	root := reporoot.Path(t)

	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-publish-stage.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var wf struct {
		Jobs map[string]struct {
			Needs []string `yaml:"needs"`
		} `yaml:"jobs"`
	}

	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}

	summarize, ok := wf.Jobs["summarize-publish-stage"]
	if !ok {
		t.Fatal("release-publish-stage.yml has no summarize-publish-stage job")
	}

	needed := make(map[string]bool, len(summarize.Needs))
	for _, n := range summarize.Needs {
		needed[n] = true
	}

	for _, target := range publishTargetKeys(t) {
		// Mirror lookupNeeds: exact key first, then the dashed spelling.
		jobID := target
		if _, exists := wf.Jobs[jobID]; !exists {
			jobID = strings.ReplaceAll(target, "_", "-")
		}

		if _, exists := wf.Jobs[jobID]; !exists {
			t.Errorf("publish-stage target %q has no job in release-publish-stage.yml (tried %q and %q); `report stage-result` would silently report it as not run",
				target, target, jobID)

			continue
		}

		if !needed[jobID] {
			t.Errorf("job %q (target %q) is missing from summarize-publish-stage.needs; its result would never reach the stage summary", jobID, target)
		}
	}
}

// publishTargetKeys reads the JSON tags off ReleasePublishTargets — the
// same names the workflow reads via fromJson(...).targets.<key>.
func publishTargetKeys(t *testing.T) []string {
	t.Helper()

	typ := reflect.TypeOf(pipeline.ReleasePublishTargets{})

	keys := make([]string, 0, typ.NumField())

	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			keys = append(keys, name)
		}
	}

	if len(keys) == 0 {
		t.Fatal("no json tags found on ReleasePublishTargets")
	}

	return keys
}

// Guard the assumption publishTargetKeys relies on: the workflow reads
// these keys out of the marshalled plan, so the tags and the JSON must
// agree.
func TestPublishStageTargets_MatchMarshalledPlan(t *testing.T) {
	body, err := json.Marshal(pipeline.ReleasePublishTargets{})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	for _, key := range publishTargetKeys(t) {
		if _, ok := got[key]; !ok {
			t.Errorf("target key %q is absent from the marshalled ReleasePublishTargets", key)
		}
	}
}
