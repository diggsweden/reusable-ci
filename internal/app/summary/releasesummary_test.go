// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func TestReleaseSummary_HappyPath(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion:      "v1.2.3",
		ReleaseBranch:       "main",       //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseCommit:       "abcdef0123", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseActor:        "bot",        //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RunURL:              "https://example.com/run/42",
		CreateReleaseResult: "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		PrepareStageJSON:    stageResultJSON(t, "prepare", map[string]string{"version_bump": "success"}),
		BuildStageJSON: stageResultJSON(t, "build", map[string]string{
			"maven":  "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"npm":    "failure", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"gradle": "skipped", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"go":     "success",
			"cargo":  "skipped", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}),
		PublishStageJSON: stageResultJSON(t, "publish", map[string]string{
			"forge_packages":        "success",
			"forge_packages_gradle": "success",
			"maven_central_gradle":  "failure",
			"xcode_ios":             "success",
			"containers":            "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"cargo_container_first": "success",
			"go_container_first":    "failure",
		}),
		URLs:       github.New(),
		ServerURL:  "https://github.com", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Repository: "owner/repo",
		Now:        fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"# Release Summary",
		"| **Version** | `v1.2.3` |",
		"| **Branch** | `main` |",
		"| **Commit** | `abcdef0123` |",
		"| **Released By** | @bot |",
		"| **Released At** | 2026-05-10 14:30:00 UTC |",
		"| Version Bump | ✓ |",
		"| Build Maven | ✓ |",
		"| Build NPM | ✗ |",
		"| Build Gradle | − |",
		"| Build Go | ✓ |",
		"| Build Cargo | − |",
		"| Publish GitHub | ✓ |",
		"| Publish Forge Packages (Gradle) | ✓ |",
		"| Publish Maven Central (Gradle) | ✗ |",
		"| Publish Apple App Store | ✓ |",
		"| Containers | ✓ |",
		"| Cargo SBOM | ✓ |",
		"| Go SBOM | ✗ |",
		"| GitHub Release | ✓ |",
		"- [Release](https://github.com/owner/repo/releases/tag/v1.2.3)",
		"- [Packages](https://github.com/owner/repo/packages)",
		"- [Workflow Run](https://example.com/run/42)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestReleaseSummary_RejectsMalformedStageResultJSON(t *testing.T) {
	t.Parallel()

	err := appsummary.ReleaseSummary(context.Background(), &fakeSummarySink{}, appsummary.ReleaseSummaryInput{
		PrepareStageJSON: `{"stage":"prepare","targets":{}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "prepare-stage result-json") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseSummary_GitLabURLs(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		URLs:           gitlab.New(),
		ServerURL:      "https://gitlab.com",
		Repository:     "group/proj",
		Now:            fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	if !strings.Contains(body, "[Release](https://gitlab.com/group/proj/-/releases/v1.0.0)") {
		t.Errorf("gitlab release URL wrong: %s", body)
	}

	if !strings.Contains(body, "[Packages](https://gitlab.com/group/proj/-/packages)") {
		t.Errorf("gitlab packages URL wrong: %s", body)
	}
}

func TestReleaseSummary_MissingStageJSONsDefaultToSkipped(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion:      "v1.0.0",
		CreateReleaseResult: "skipped",
		URLs:                github.New(),
		ServerURL:           "https://github.com",
		Repository:          "o/r",
		Now:                 fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// All rows render as − (skipped) when no stage JSONs and the release row
	// is also "skipped".
	if strings.Contains(sink.buf.String(), "| ✗ |") {
		t.Errorf("no failures expected when all results are skipped: %s", sink.buf.String())
	}
}
