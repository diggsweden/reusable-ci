// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
		"| Publish Forge Packages | ✓ |",
		"| Publish Apple App Store | ✓ |",
		"| Containers | ✓ |",
		"| Cargo SBOM | ✓ |",
		"| Go SBOM | ✗ |",
		"| Forge Release | ✓ |",
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

	sink := &fakeSummarySink{}

	err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		PrepareStageJSON: `{"stage":"prepare","targets":{}}`,
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	// The message has to name which of the three stage inputs was bad.
	if !strings.Contains(err.Error(), "prepare-stage result-json") {
		t.Errorf("err = %v, want it to name the prepare stage", err)
	}

	if sink.buf.Len() != 0 {
		t.Errorf("appended a summary despite the rejected input:\n%s", sink.buf.String())
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
	// The name is the claim: absent stage JSONs must render as skipped. The
	// old check only ruled out ✗, which a summary that dropped the Job Status
	// table altogether would also have satisfied.
	body := sink.buf.String()
	for _, want := range []string{
		"| Version Bump | − |",
		"| Build Maven | − |",
		"| Containers | − |",
		"| Forge Release | − |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}

	for _, unwanted := range []string{"| ✗ |", "| ✓ |"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected %q when every result is skipped:\n%s", unwanted, body)
		}
	}
}

// TestReleaseSummary_NoWebUIRendersNoBrokenLinks drives the whole summary on a
// platform without a web UI and with no run URL, the case that produced
// "[Release]((release: v1.2.3))", "[Packages]((packages))" and
// "[Workflow Run]()".
func TestReleaseSummary_NoWebUIRendersNoBrokenLinks(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.ReleaseSummary(context.Background(), sink, appsummary.ReleaseSummaryInput{
		ReleaseVersion: "v1.2.3",
		Repository:     "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"- Release: not available\n",
		"- Packages: not available\n",
		"- Workflow Run: not available\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary missing %q:\n%s", want, body)
		}
	}

	for _, broken := range []string{"]((", "]()"} {
		if strings.Contains(body, broken) {
			t.Errorf("summary contains a malformed link %q:\n%s", broken, body)
		}
	}
}
