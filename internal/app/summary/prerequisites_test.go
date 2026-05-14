// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/domain/git"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// fakeGitInfo is an in-memory gitInfoOps for prerequisites-summary tests.
type fakeGitInfo struct {
	taggerName string
	taggerDate string
	tagMessage string
	tagBody    string
	verifyOK   bool
	commit     git.CommitInfo
}

func (f *fakeGitInfo) TaggerInfo(_ context.Context, _ string) (string, string, error) {
	return f.taggerName, f.taggerDate, nil
}
func (f *fakeGitInfo) TagMessage(_ context.Context, _ string) (string, error) {
	return f.tagMessage, nil
}
func (f *fakeGitInfo) CatFileTag(_ context.Context, _ string) (string, error) {
	return f.tagBody, nil
}
func (f *fakeGitInfo) VerifyTag(_ context.Context, _ string) (string, bool, error) {
	return "", f.verifyOK, nil
}
func (f *fakeGitInfo) CommitInfo(_ context.Context, _ string) (git.CommitInfo, error) {
	return f.commit, nil
}

func TestPrerequisites_TagSection(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	gitr := &fakeGitInfo{
		taggerName: "Bot <bot@example.invalid>",
		taggerDate: "2026-05-10",
		tagMessage: "Release v1.0.0\nbody",
		verifyOK:   true,
		commit: git.CommitInfo{
			Author:  "Alice <alice@example.invalid>",
			Date:    "2026-05-09",
			Message: "fix: bug",
			Body:    "tree abc\nparent def\nauthor Alice\n",
		},
	}
	err := appsummary.Prerequisites(context.Background(), sink, gitr, appsummary.PrerequisitesInput{
		TagName:         "v1.0.0",
		CommitSHA:       "abcdef0123",
		RefType:         provider.RefTypeTag,
		HasReleaseToken: true,
		JobStatus:       domainsummary.ResultSuccess,
		Now:             fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	for _, want := range []string{
		"# 📋 Release Prerequisites Validation Report",
		"## 🏷️ Release Tag",
		"- **Tag:** `v1.0.0`",
		"- **Type:** tag",
		"- **Tagger:** Bot <bot@example.invalid>",
		"- **Tag Date:** 2026-05-10",
		"- **Tag Signature:** GPG signed",
		"- **Tag Message:** Release v1.0.0", // first line only
		"## 📦 Tagged Commit",
		"- **SHA:** `abcdef0123`",
		"- **Author:** Alice <alice@example.invalid>",
		"- **Date:** 2026-05-09",
		"### ✅ All required prerequisites are configured!",
		"| Release Type | 🎯 Stable | Production release |",
		"| Release Token | ✓ Pass | Valid GitHub token |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestPrerequisites_PrereleaseTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "rc", tag: "v1.0.0-rc.2", want: "| Release Type | 🚧 Pre-release | `rc` version |"},
		{name: "alpha", tag: "v1.0.0-alpha", want: "| Release Type | 🚧 Pre-release | `alpha` version |"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			sink := &fakeSummarySink{}
			err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
				TagName: testCase.tag,
				RefType: provider.RefTypeTag,
				Now:     fixedNow(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sink.buf.String(), testCase.want) {
				t.Errorf("expected prerelease row %q:\n%s", testCase.want, sink.buf.String())
			}
		})
	}
}

func TestPrerequisites_SnapshotSkipsAuthorization(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:            "v1.0.0-SNAPSHOT",
		RefType:            provider.RefTypeTag,
		CheckAuthorization: false,
		Now:                fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.buf.String(), "| User Authorization | − Skip | SNAPSHOT release |") {
		t.Errorf("expected SNAPSHOT skip row:\n%s", sink.buf.String())
	}
}

func TestPrerequisites_GPGSecretsGatedByFlag(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:                 "v1.0.0",
		RefType:                 provider.RefTypeTag,
		SignArtifacts:           true,
		HasReleaseGPGPrivateKey: true,
		HasReleaseGPGPassphrase: false,
		HasReleaseGPGPublicKey:  true,
		Now:                     fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	for _, want := range []string{
		"| RELEASE_GPG_PRIVATE_KEY | Sign commits/artifacts | ✓ Available |",
		"| RELEASE_GPG_PASSPHRASE | GPG passphrase | ✗ Missing |",
		"| RELEASE_GPG_PUBLIC_KEY | GPG verification | ✓ Available |",
		"| **GPG Signing** | Enabled |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestPrerequisites_SignArtifactsFalseHidesGPG(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:       "v1.0.0",
		RefType:       provider.RefTypeTag,
		SignArtifacts: false,
		Now:           fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.buf.String(), "RELEASE_GPG_PRIVATE_KEY") {
		t.Errorf("GPG secrets should be hidden when SignArtifacts=false: %s", sink.buf.String())
	}
}

func TestPrerequisites_PublishToTargets(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:                 "v1.0.0",
		RefType:                 provider.RefTypeTag,
		PublishTo:               "maven-central,github-packages",
		HasMavenCentralUsername: true,
		HasMavenCentralPassword: true,
		Now:                     fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	for _, want := range []string{
		"| MAVEN_CENTRAL_USERNAME | Maven Central auth | ✓ Available |",
		"| MAVEN_CENTRAL_PASSWORD | Maven Central auth | ✓ Available |",
		"| Maven Central | ✓ Pass | Credentials configured |",
		"| GitHub Packages | ✓ Pass | Using GITHUB_TOKEN |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
	// NPM not in PublishTo → no NPM rows.
	if strings.Contains(body, "NPM_TOKEN") {
		t.Errorf("NPM_TOKEN should not appear when PublishTo doesn't include npmjs: %s", body)
	}
}

func TestPrerequisites_BranchRefSkipsTagSections(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName: "main",
		RefType: provider.RefTypeBranch,
		Now:     fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	if strings.Contains(body, "Tagger:") {
		t.Errorf("branch ref should not show tagger line: %s", body)
	}
	if strings.Contains(body, "Semantic Version") {
		t.Errorf("branch ref should not validate semver: %s", body)
	}
}

func TestPrerequisites_FailedJobStatus(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:   "v1.0.0",
		RefType:   provider.RefTypeTag,
		JobStatus: domainsummary.ResultFailure,
		Now:       fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.buf.String(), "### ❌ Prerequisites validation failed") {
		t.Errorf("expected failure header: %s", sink.buf.String())
	}
}

func TestPrerequisites_MissingReleaseTokenShown(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.Prerequisites(context.Background(), sink, &fakeGitInfo{}, appsummary.PrerequisitesInput{
		TagName:         "v1.0.0",
		RefType:         provider.RefTypeTag,
		HasReleaseToken: false,
		Now:             fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	if !strings.Contains(body, "| Release Token | ✗ Fail | Missing RELEASE_TOKEN |") {
		t.Errorf("missing release token row: %s", body)
	}
}
