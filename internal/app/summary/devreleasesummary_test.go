// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestDevReleaseSummary_NPMHappyPath(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	var out bytes.Buffer

	err := appsummary.DevReleaseSummary(context.Background(), sink, &out, appsummary.DevReleaseSummaryInput{
		ProjectType:       projecttype.NPM,
		ReleaseRef:        "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseSHA:        "abcdef0123", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseActor:      "bot", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseRepository: "owner/repo",
		RunURL:            "https://example.com/run/1", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		BuildStageJSON:    stageResultJSON(t, "dev-build", map[string]string{"npm": "success"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishStageJSON:  stageResultJSON(t, "dev-publish", map[string]string{"containers": "success", "npm": "success"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		DevArtifactsJSON:  `{"npm_package_name":"my-pkg","npm_package_version":"0.0.0-dev.abc"}`,
		Platform:          provider.PlatformGitHub,
		ServerURL:         "https://github.com", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Now:               fixedNow(),
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
		"| Build Container | ✓ |",
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

func TestDevReleaseSummary_ShowsBuildAndSBOMRows(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:    projecttype.Go,
		BuildStageJSON: stageResultJSON(t, "dev-build", map[string]string{"go": "failure"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{
			"containers":         "success",
			"go_container_first": "success",
			"sbom":               "success",
		}),
		Platform:  provider.PlatformGitHub,
		ServerURL: "https://github.com",
		Now:       fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"| Build Go | ✗ |",
		"| Build Container | ✓ |",
		"| Go SBOM | ✓ |",
		"| Dev SBOMs | ✓ |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestDevReleaseSummary_NPMAlreadyExistsNote(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:      projecttype.NPM,
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"containers": "success", "npm": "success"}),
		DevArtifactsJSON: `{"npm_package_name":"x","npm_package_version":"0.0.0-dev","npm_publish_status":"already-exists"}`,
		Platform:         provider.PlatformGitHub,
		ServerURL:        "https://github.com",
		Now:              fixedNow(),
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

func TestDevReleaseSummary_NonNPMProjectHidesNPMSections(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:      projecttype.Go,
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"containers": "success"}),
		Platform:         provider.PlatformGitHub,
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

func TestDevReleaseSummary_ShowsContainerRowAndResources(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:       projecttype.NPM,
		ReleaseRef:        "feat/dev-branch",
		ReleaseSHA:        "def7890abcdef",
		ReleaseActor:      "dev-user",
		ReleaseRepository: "org/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RunURL:            "https://example.com/run/1",
		PublishStageJSON:  stageResultJSON(t, "dev-publish", map[string]string{"containers": "success", "npm": "success"}),
		DevArtifactsJSON:  `{"npm_package_name":"@org/pkg","npm_package_version":"1.0.0-dev","npm_publish_status":"published"}`,
		Platform:          provider.PlatformGitHub,
		ServerURL:         "https://github.com",
		Now:               fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{"Build Container", "Resources", "Packages", "Workflow Run"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestDevReleaseSummary_NPMNotPublishedFallback(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:      projecttype.NPM,
		PublishStageJSON: stageResultJSON(t, "dev-publish", map[string]string{"npm": "failure"}),
		Platform:         provider.PlatformGitHub,
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

func TestDevReleaseSummary_RejectsMalformedStageResultJSON(t *testing.T) {
	t.Parallel()

	err := appsummary.DevReleaseSummary(context.Background(), &fakeSummarySink{}, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		BuildStageJSON: `{"stage":"dev-build","targets":{}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "dev-build-stage result-json") {
		t.Fatalf("err = %v", err)
	}
}
