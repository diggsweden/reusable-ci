// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	summarycmd "github.com/diggsweden/reusable-ci/internal/cli/commands/summary"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestPRCmd_PrefersPathOverInlineJSON(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("quality.json", []byte(`{"targets":{"dependencyreview":"skipped","sastopengrep":"failure","publiccodelint":"success","devbasecheck":"failure","swift":"skipped"}}`))
	env.Setenv("PROJECT_TYPE", "maven")
	env.Setenv("CI_BRANCH", "feat/my-branch")
	env.Setenv("CI_COMMIT", "abc1234567890")
	env.Setenv("CI_ACTOR", "test-user")
	env.Setenv("CI_RUN_URL", "https://example.com/run/1")
	env.Setenv("QUALITY_STAGE_RESULT_PATH", path)
	env.Setenv("QUALITY_STAGE_RESULT_JSON", `{"targets":{"dependencyreview":"success","sastopengrep":"success","publiccodelint":"success","devbasecheck":"success","swift":"success"}}`)

	cmd := summarycmd.New()
	if err := cmd.Run(context.Background(), []string{"summary", "pr"}); err != nil {
		t.Fatal(err)
	}
	body := env.Summary()
	for _, want := range []string{"| Devbase Check | ✗ |", "| OpenGrep SAST | ✗ |"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestQualityCheckStatusCmd_NoArgsStillRendersSummary(t *testing.T) {
	env := ghaenv.Setup(t)
	cmd := summarycmd.New()
	if err := cmd.Run(context.Background(), []string{"summary", "quality-check-status"}); err != nil {
		t.Fatal(err)
	}
	body := env.Summary()
	for _, want := range []string{"Pull Request Check Status", "Quality Check Results"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestBuildStageResultCmd_WritesOutputsAndManifest(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	resultsDir := fsys.MkdirAll("ci-results")
	env.Setenv("CI_RESULTS_DIR", resultsDir)
	env.Setenv("PROJECT_TYPE", "maven")
	env.Setenv("BUILD_MAVEN_RESULT", "success")
	env.Setenv("MAVEN_ARTIFACTS", `["target/app.jar"]`)

	cmd := summarycmd.New()
	if err := cmd.Run(context.Background(), []string{"summary", "build-stage-result"}); err != nil {
		t.Fatal(err)
	}
	if got := env.Output("stage-ran"); got != "true" {
		t.Errorf("stage-ran = %q", got)
	}
	if got := env.Output("stage-result"); got != "success" {
		t.Errorf("stage-result = %q", got)
	}
	if got := env.Output("result-json"); !strings.Contains(got, `"stage":"build"`) {
		t.Errorf("result-json = %q", got)
	}
	data := fsys.ReadFile("ci-results/build-result.json")
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["stage"] != "build" || doc["result"] != "success" {
		t.Errorf("manifest = %v", doc)
	}
	if _, err := os.Stat(fsys.Path("ci-results", "build-result.json")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}
