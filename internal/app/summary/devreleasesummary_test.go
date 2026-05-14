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
	var stdout bytes.Buffer
	err := appsummary.DevReleaseSummary(context.Background(), sink, &stdout, appsummary.DevReleaseSummaryInput{
		ProjectType:       projecttype.NPM,
		ReleaseRef:        "main",
		ReleaseSHA:        "abcdef0123",
		ReleaseActor:      "bot",
		ReleaseRepository: "owner/repo",
		RunURL:            "https://example.com/run/1",
		PublishStageJSON:  `{"targets":{"container":"success","npm":"success"}}`,
		DevArtifactsJSON:  `{"targets":{"npm_package_name":"my-pkg","npm_package_version":"0.0.0-dev.abc"}}`,
		Platform:          provider.PlatformGitHub,
		ServerURL:         "https://github.com",
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
	if !strings.Contains(stdout.String(), "Generating Dev Release Summary") {
		t.Errorf("stdout should have banner: %s", stdout.String())
	}
}

func TestDevReleaseSummary_NPMAlreadyExistsNote(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.DevReleaseSummary(context.Background(), sink, &bytes.Buffer{}, appsummary.DevReleaseSummaryInput{
		ProjectType:      projecttype.NPM,
		PublishStageJSON: `{"targets":{"container":"success","npm":"success"}}`,
		DevArtifactsJSON: `{"targets":{"npm_package_name":"x","npm_package_version":"0.0.0-dev","npm_publish_status":"already-exists"}}`,
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
		PublishStageJSON: `{"targets":{"container":"success"}}`,
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
		ReleaseRepository: "org/repo",
		RunURL:            "https://example.com/run/1",
		PublishStageJSON:  `{"targets":{"container":"success","npm":"success"}}`,
		DevArtifactsJSON:  `{"targets":{"npm_package_name":"@org/pkg","npm_package_version":"1.0.0-dev","npm_publish_status":"published"}}`,
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
		PublishStageJSON: `{"targets":{"npm":"failure"}}`,
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
