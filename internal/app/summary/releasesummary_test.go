// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestReleaseSummary_HappyPath(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion:      "v1.2.3",
		ReleaseBranch:       "main",
		ReleaseCommit:       "abcdef0123",
		ReleaseActor:        "bot",
		RunURL:              "https://example.com/run/42",
		CreateReleaseResult: "success",
		PrepareStageJSON:    `{"targets":{"version-bump":"success"}}`,
		BuildStageJSON:      `{"targets":{"maven":"success","npm":"failure","gradle":"skipped"}}`,
		PublishStageJSON:    `{"targets":{"githubpackages":"success","containers":"success"}}`,
		Platform:            provider.PlatformGitHub,
		ServerURL:           "https://github.com",
		Repository:          "owner/repo",
		Now:                 fixedNow(),
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
		"| Publish GitHub | ✓ |",
		"| Containers | ✓ |",
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

func TestReleaseSummary_GitLabURLs(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion: "v1.0.0",
		Platform:       provider.PlatformGitLab,
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
		Platform:            provider.PlatformGitHub,
		ServerURL:           "https://github.com",
		Repository:          "o/r",
		Now:                 fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// All twelve rows render as − (skipped) when no stage JSONs and the
	// release row is also "skipped".
	if strings.Contains(sink.buf.String(), "| ✗ |") {
		t.Errorf("no failures expected when all results are skipped: %s", sink.buf.String())
	}
}
