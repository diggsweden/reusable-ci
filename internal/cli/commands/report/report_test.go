// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package report_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	reportcmd "github.com/diggsweden/reusable-ci/internal/cli/commands/report"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestPRCmd_PrefersPathOverInlineJSON(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("quality.json", []byte(`{"version":1,"stage":"pr-quality","result":"failure","ran":true,"targets":{"nanolinter":"failure","swift":"skipped"}}`))

	env.Setenv("PROJECT_TYPE", "maven")
	env.Setenv("CI_BRANCH", "feat/my-branch")
	env.Setenv("CI_COMMIT", "abc1234567890")
	env.Setenv("CI_ACTOR", "test-user")
	env.Setenv("CI_RUN_URL", "https://example.com/run/1")
	env.Setenv("QUALITY_STAGE_RESULT_PATH", path)
	env.Setenv("QUALITY_STAGE_RESULT_JSON", `{"version":1,"stage":"pr-quality","result":"success","ran":true,"targets":{"nanolinter":"success","swift":"success"}}`)

	cmd := reportcmd.New()
	if err := cmd.Run(context.Background(), []string{"report", "pr"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	body := env.Summary()
	for _, want := range []string{"| Nanolinter | ✗ |"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestQualityCheckStatusCmd_NoArgsStillRendersSummary(t *testing.T) {
	env := ghaenv.Setup(t)

	cmd := reportcmd.New()
	if err := cmd.Run(context.Background(), []string{"report", "status", "quality-check"}); err != nil {
		t.Fatal(err)
	}

	body := env.Summary()
	for _, want := range []string{"Pull Request Check Status", "Quality Check Results"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestStageResultCmd_WritesOutputsAndManifest(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	resultsDir := fsys.MkdirAll("ci-results")
	env.Setenv("CI_RESULTS_DIR", resultsDir)
	env.Setenv("STAGE_PLAN_JSON", `{"version":1,"stage":"build","targets":{"maven":{"runs":true},"npm":{"runs":false}}}`)

	cmd := reportcmd.New()
	if err := cmd.Run(context.Background(), []string{"report", "status", "stage", "--result", "maven=success", "--extra", "project_type=maven"}); err != nil {
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
