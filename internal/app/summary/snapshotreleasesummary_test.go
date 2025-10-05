// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestSnapshotReleaseSummary_NPMHappyPath(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	var out bytes.Buffer

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &out, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:           projecttype.NPM,
		ReleaseRef:            "main",       //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseSHA:            "abcdef0123", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseActor:          "bot",        //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseRepository:     "owner/repo",
		RunURL:                "https://example.com/run/1",                                                                     //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		BuildStageJSON:        stageResultJSON(t, "dev-build", map[string]string{"npm": "success"}),                            //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishStageJSON:      stageResultJSON(t, "dev-publish", map[string]string{"containers": "success", "npm": "success"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		SnapshotArtifactsJSON: `{"npm_package_name":"my-pkg","npm_package_version":"0.0.0-dev.abc"}`,
		URLs:                  github.New(),
		ServerURL:             "https://github.com", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Now:                   fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"# Dev Release Summary",
		"| **Project Type** | `npm` |",
		"| **Branch** | `main` |",
		"| **Commit** | `abcdef0` |",
		"| Build NPM | ✓ |",
		"| Publish NPM Package | ✓ |",
		"### NPM Package",
		"my-pkg@0.0.0-dev.abc",
		"npm install my-pkg@0.0.0-dev.abc",
		"npm install my-pkg@dev",
		"- [Packages](https://github.com/owner/repo/packages)",
		"- [Workflow Run](https://example.com/run/1)",
		"These are development artifacts",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}

	if !strings.Contains(out.String(), "Generating Dev Release Summary") {
		t.Errorf("out should have banner: %s", out.String())
	}
}

func TestSnapshotReleaseSummary_ShowsBuildAndSBOMRows(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:    projecttype.Go,
		BuildStageJSON: stageResultJSON(t, "dev-build", map[string]string{"go": "failure"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{
			"go_container_first": "success",
			"sbom":               "success",
		}),
		URLs:      github.New(),
		ServerURL: "https://github.com",
		Now:       fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"| Build Go | ✗ |",
		"| Go SBOM | ✓ |",
		"| Dev SBOMs | ✓ |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestSnapshotReleaseSummary_NPMAlreadyExistsNote(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:           projecttype.NPM,
		PublishStageJSON:      stageResultJSON(t, "dev-publish", map[string]string{"containers": "success", "npm": "success"}),
		SnapshotArtifactsJSON: `{"npm_package_name":"x","npm_package_version":"0.0.0-dev","npm_publish_status":"already-exists"}`,
		URLs:                  github.New(),
		ServerURL:             "https://github.com",
		Now:                   fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	if !strings.Contains(body, "(already published — skipped)") {
		t.Errorf("expected job-status note: %s", body)
	}

	if !strings.Contains(body, "Version already existed in registry") {
		t.Errorf("expected published-artifacts note: %s", body)
	}
}

func TestSnapshotReleaseSummary_NonNPMProjectHidesNPMSections(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:      projecttype.Go,
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"containers": "success"}),
		URLs:             github.New(),
		ServerURL:        "https://github.com",
		Now:              fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	if strings.Contains(body, "Publish NPM Package") {
		t.Errorf("non-npm project should not show NPM job row: %s", body)
	}

	if strings.Contains(body, "### NPM Package") {
		t.Errorf("non-npm project should not show NPM artifacts section: %s", body)
	}
}

// TestSnapshotReleaseSummary_RendersBuildInformationAndResources covers the
// header block and the resource links, which are derived from the release ref,
// SHA, actor and repository rather than from any stage result.
//
// It was previously named for a container row, and the assertions never
// looked for one. There is none to look for, and that is correct rather than a
// gap: pipeline.DevPublishTargets has no containers field, because the dev
// flow builds no containers -- they are promoted to :dev on the release path
// by the build-once/promote-many ladder. ReleaseSummary renders a "Containers"
// row because its stage plan has that target; this one does not.
func TestSnapshotReleaseSummary_RendersBuildInformationAndResources(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:           projecttype.NPM,
		ReleaseRef:            "feat/dev-branch",
		ReleaseSHA:            "def7890abcdef",
		ReleaseActor:          "dev-user",
		ReleaseRepository:     "org/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RunURL:                "https://example.com/run/1",
		PublishStageJSON:      stageResultJSON(t, "dev-publish", map[string]string{"npm": "success"}),
		SnapshotArtifactsJSON: `{"npm_package_name":"@org/pkg","npm_package_version":"1.0.0-dev","npm_publish_status":"published"}`,
		URLs:                  github.New(),
		ServerURL:             "https://github.com",
		Now:                   fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Whole rows and whole links. The bare words "Resources" and "Packages"
	// appear in the block whatever the repository is, so they said nothing
	// about the ref, actor or repository the fixture supplies.
	body := sink.buf.String()
	for _, want := range []string{
		"| **Branch** | `feat/dev-branch` |",
		"| **Commit** | `def7890` |", // truncated to 7
		"| **Built By** | @dev-user |",
		"| **Built At** | 2026-05-10 14:30:00 UTC |",
		"- [Packages](https://github.com/org/repo/packages)",
		"- [Workflow Run](https://example.com/run/1)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestSnapshotReleaseSummary_NPMNotPublishedFallback(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		ProjectType:      projecttype.NPM,
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"npm": "failure"}),
		URLs:             github.New(),
		ServerURL:        "https://github.com",
		Now:              fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "### NPM Package\nNot published") {
		t.Errorf("expected 'Not published' fallback: %s", sink.buf.String())
	}
}

func TestSnapshotReleaseSummary_RejectsMalformedStageResultJSON(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.SnapshotReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.SnapshotReleaseSummaryInput{
		BuildStageJSON: `{"stage":"dev-build","targets":{}}`,
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	if !strings.Contains(err.Error(), "snapshot-build-stage result-json") {
		t.Errorf("err = %v, want it to name the build stage", err)
	}

	if sink.buf.Len() != 0 {
		t.Errorf("appended a summary despite the rejected input:\n%s", sink.buf.String())
	}
}
