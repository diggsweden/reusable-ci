// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestStageSlotBoundary_IdentityBeforeAllOutput(t *testing.T) {
	t.Parallel()

	for _, slot := range []string{"prepare", "build", "publish", "pr-quality", "dev-build", "dev-publish"} {
		for _, supplied := range []string{"", slot, "wrong-stage"} {
			body := ""
			if supplied != "" {
				body = stageResultJSON(t, supplied, map[string]string{"npm": "success"})
			}

			sink := &fakeSummarySink{}

			var (
				out bytes.Buffer
				err error
			)

			switch slot {
			case "prepare", "build", "publish":
				in := appsummary.ReleaseSummaryInput{Now: fixedNow()}

				switch slot {
				case "prepare":
					in.PrepareStageJSON = body
				case "build":
					in.BuildStageJSON = body
				case "publish":
					in.PublishStageJSON = body
				}

				err = appsummary.ReleaseSummary(t.Context(), sink, in)
			case "pr-quality":
				err = appsummary.PRSummary(t.Context(), sink, appsummary.PRSummaryInput{QualityStageResultJSON: body, Now: fixedNow()})
			default:
				in := appsummary.SnapshotReleaseSummaryInput{Now: fixedNow()}
				if slot == "dev-build" {
					in.BuildStageJSON = body
				} else {
					in.PublishStageJSON = body
				}

				err = appsummary.SnapshotReleaseSummary(t.Context(), sink, &out, in)
			}

			if supplied == "wrong-stage" {
				if !errors.Is(err, errs.ErrMalformedInput) || sink.buf.Len() != 0 || out.Len() != 0 {
					t.Fatalf("slot=%s err=%v summary=%s out=%s", slot, err, &sink.buf, &out)
				}
			} else if err != nil || sink.buf.Len() == 0 {
				t.Fatalf("slot=%s supplied=%s err=%v", slot, supplied, err)
			}
		}
	}
}

func TestSnapshotMetadataBoundary_ShapeAndSnippetGrammar(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", `{"npm_package_name":"@org/fixture","npm_package_version":"1.2.3-snapshot-test","extra":7}`, `{"npm_package_name":"fixture"}`, `null`, `{`, "{\"npm_package_name\":\"fixture\\n```\\n## injected\",\"npm_package_version\":\"1.2.3\"}", `{"npm_package_name":"fixture","npm_package_version":"1.2.3; echo injected"}`} {
		sink := &fakeSummarySink{}

		var out bytes.Buffer

		err := appsummary.SnapshotReleaseSummary(t.Context(), sink, &out, appsummary.SnapshotReleaseSummaryInput{ProjectType: projecttype.NPM, PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"npm": "success"}), SnapshotArtifactsJSON: body, Now: fixedNow()})

		valid := body == "" || strings.Contains(body, `"extra":7`)
		if !valid { //nolint:nestif // malformed, absent and complete successful metadata intentionally have different output contracts.
			if !errors.Is(err, errs.ErrMalformedInput) || sink.buf.Len() != 0 || out.Len() != 0 {
				t.Fatalf("bad metadata=%s err=%v summary=%s out=%s", body, err, &sink.buf, &out)
			}
		} else {
			if err != nil || strings.Contains(sink.buf.String(), "Not published") {
				t.Fatalf("valid metadata err=%v summary=%s", err, &sink.buf)
			}

			if body == "" && !strings.Contains(sink.buf.String(), "metadata unavailable") {
				t.Fatal("absent metadata not distinguished")
			}

			if body != "" && !strings.Contains(sink.buf.String(), "npm install @org/fixture@1.2.3-snapshot-test") {
				t.Fatal("valid package/version not preserved")
			}
		}
	}
}

func TestPRCancellationBoundary_BothEngines(t *testing.T) {
	t.Parallel()

	for _, engine := range []string{"nanolinter", "megalinter"} {
		t.Run(engine, func(t *testing.T) {
			for _, status := range []string{"success", "failure", "cancelled", "skipped"} {
				sink := &fakeSummarySink{}
				if err := appsummary.PRSummary(t.Context(), sink, appsummary.PRSummaryInput{Now: fixedNow(), QualityStageResultJSON: stageResultJSON(t, "pr-quality", map[string]string{engine: status})}); err != nil {
					t.Fatal(err)
				}

				label := "Nanolinter"
				if engine == "megalinter" {
					label = "MegaLinter"
				}

				icon := "\u2717"
				if status == "success" {
					icon = "\u2713"
				}

				if status == "skipped" {
					label = "Lint"
					icon = "\u2212"
				}

				row := "| " + label + " | " + icon + " |\n"
				if strings.Count(sink.buf.String(), row) != 1 {
					t.Fatalf("missing exact engine outcome %q in %s", row, &sink.buf)
				}
			}
		})
	}
}

func TestStageFixtureBoundary_AggregateAndRan(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		targets map[string]string
		result  string
		ran     bool
	}{
		{map[string]string{}, "skipped", false}, {map[string]string{"x": "skipped"}, "skipped", false},
		{map[string]string{"x": "success", "y": "skipped"}, "success", true},
		{map[string]string{"x": "cancelled", "y": "success"}, "cancelled", true},
		{map[string]string{"x": "failure", "y": "success", "z": "cancelled"}, "failure", true},
	} {
		var got struct {
			Result string `json:"result"`
			Ran    bool   `json:"ran"`
		}
		if err := json.Unmarshal([]byte(stageResultJSON(t, "build", tc.targets)), &got); err != nil {
			t.Fatal(err)
		}

		if got.Result != tc.result || got.Ran != tc.ran {
			t.Fatalf("targets=%v result=%+v", tc.targets, got)
		}
	}
}

func TestStageOutputBoundary_ExactAllChannels(t *testing.T) {
	t.Parallel()

	for _, ran := range []bool{true, false} {
		out, manifest, jobs := fakeoutputsink.New(t), fakemanifestsink.New(t), fakejobresultstore.New(t)

		env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, appsummary.StageResultInput{StagePlanJSON: fmt.Sprintf(`{"version":1,"stage":"build","targets":{"npm":{"runs":%t}}}`, ran), Results: []domainsummary.KeyValue{{Key: "npm", Value: "success"}}, JSONOutputKey: "artifacts-json", JSONFields: []domainsummary.KeyValue{{Key: "name", Value: "fixture"}, {Key: "version", Value: "2.3.4"}}})
		if err != nil {
			t.Fatal(err)
		}

		body, err := env.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}

		result, run := "skipped", "false"
		if ran {
			result, run = "success", "true"
		}

		want := map[string]string{"stage-result": result, "stage-ran": run, "result-json": string(body), "artifacts-json": `{"name":"fixture","version":"2.3.4"}`}
		if !reflect.DeepEqual(out.AllScalar(), want) || !reflect.DeepEqual(manifest.Stages(), []string{"build"}) || manifest.Body("build") != string(body) {
			t.Fatalf("outputs=%v manifest=%s", out.AllScalar(), manifest.Body("build"))
		}
	}
}

func TestBOMCountBoundary_RelativeExclusionsAndFileTypes(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "target", "project")
	if err := os.MkdirAll(filepath.Join(root, "target"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "target/bom.json"), []byte("excluded"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("missing", filepath.Join(root, "bom.json")); err != nil {
		t.Fatal(err)
	}

	sink := &fakeSummarySink{}
	if err := appsummary.SBOMCountStatus(t.Context(), sink, appsummary.SBOMCountStatusInput{Kind: "cargo", Outcome: "success", WorkDir: root}); err != nil {
		t.Fatal(err)
	}

	if sink.buf.String() != "### Build SBOM\n- \u26a0\ufe0f cargo-cyclonedx reported success but produced no bom.json\n" {
		t.Fatalf("nonexistent/excluded BOM counted: %s", &sink.buf)
	}

	if err := os.Remove(filepath.Join(root, "bom.json")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "bom.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	sink = &fakeSummarySink{}
	if err := appsummary.SBOMCountStatus(t.Context(), sink, appsummary.SBOMCountStatusInput{Kind: "cargo", Outcome: "success", WorkDir: root}); err != nil {
		t.Fatal(err)
	}

	if sink.buf.String() != "### Build SBOM\n- \u2713 CycloneDX: 1 bom.json file(s)\n" {
		t.Fatalf("parent target affected count: %s", &sink.buf)
	}
}

func TestPromotionRowsBoundary_CompleteAndNoncontradictory(t *testing.T) {
	t.Parallel()

	for _, unset := range []bool{false, true} {
		in := appsummary.ReleaseSummaryInput{Now: fixedNow(), CreateReleaseResult: "skipped"}

		icons := []string{"\u2713", "\u2717", "\u2717"}
		if unset {
			icons = []string{"\u2212", "\u2212", "\u2212"}
		} else {
			in.PromoteDevResult = "success"
			in.PromoteStagingResult = "failure"
			in.PromoteReleaseResult = "cancelled"
		}

		sink := &fakeSummarySink{}
		if err := appsummary.ReleaseSummary(t.Context(), sink, in); err != nil {
			t.Fatal(err)
		}

		for i, stage := range []string{"dev", "staging", "release"} {
			prefix := "| Promote Image \u2192 " + stage + " |"

			row := prefix + " " + icons[i] + " |\n"
			if strings.Count(sink.buf.String(), prefix) != 1 || !strings.Contains(sink.buf.String(), row) {
				t.Fatalf("row=%q summary=%s", row, &sink.buf)
			}
		}
	}
}

type metadataContextKey struct{}

func TestPrerequisiteMetadataBoundary_SuppressedCalls(t *testing.T) {
	t.Parallel()

	for _, tag := range []bool{false, true} {
		repo := &fakeGitInfo{}

		in := appsummary.PrerequisitesSummaryInput{Now: fixedNow(), RefType: provider.RefTypeBranch}
		if tag {
			in.RefType = provider.RefTypeTag
			in.TagName = "v2.3.4"
		}

		ctx := context.WithValue(t.Context(), metadataContextKey{}, "owned-context")
		if err := appsummary.Prerequisites(ctx, &fakeSummarySink{}, repo, in); err != nil {
			t.Fatal(err)
		}

		want := []string(nil)
		if tag {
			want = []string{"tagger v2.3.4", "message v2.3.4", "body v2.3.4"}
		}

		if !reflect.DeepEqual(repo.calls, want) {
			t.Fatalf("calls=%v want=%v", repo.calls, want)
		}

		for _, got := range repo.contexts {
			if got != ctx {
				t.Fatal("context not forwarded")
			}
		}
	}

	if err := appsummary.Prerequisites(t.Context(), &fakeSummarySink{}, nil, appsummary.PrerequisitesSummaryInput{RefType: provider.RefTypeTag, Now: fixedNow()}); err != nil {
		t.Fatal(err)
	}
}
