// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type releaseBoundaryRepo struct {
	fakeChangelogReleaseRepo
	inspections  int
	remoteExists bool
}

func (f *releaseBoundaryRepo) StatusPorcelain(ctx context.Context, path string) (string, error) {
	f.inspections++

	return f.fakeChangelogReleaseRepo.StatusPorcelain(ctx, path)
}
func (f *releaseBoundaryRepo) RemoteTagCommitIfExists(_ context.Context, _, _ string, _ runcontext.Credential) (string, bool, error) {
	f.inspections++

	return strings.Repeat("a", 40), f.remoteExists, nil
}

func TestReleaseBoundary_SubjectAndTagPreflight(t *testing.T) {
	for _, subject := range []string{"chore(release): bump to v1.2.3\n\nrelease body\n", "chore(release): bump to v9.9.9\n", "", "chore(release): bump to v1.2.3\r\n"} {
		dir := t.TempDir()
		t.Chdir(dir)
		writeFile(t, dir, "CHANGELOG.md", "# changes\n")
		writeFile(t, dir, "commit-msg.txt", subject)

		repo := &releaseBoundaryRepo{fakeChangelogReleaseRepo: fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}}

		var out bytes.Buffer

		_, err := appversion.ChangelogRelease(t.Context(), repo, fakeoutputsink.New(t), &out, appversion.ChangelogReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", GitURL: "ssh://git@forge.example/owner/repo.git", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid", SigningKeyPath: filepath.Join(dir, "fake-key")})
		if strings.HasSuffix(subject, "release body\n") {
			if err != nil || repo.commit.MessageFile != "commit-msg.txt" || repo.tagged != "v1.2.3" {
				t.Fatalf("err=%v repo=%+v", err, repo)
			}
		} else if !errors.Is(err, errs.ErrValidation) || repo.inspections != 0 || len(repo.cfg) != 0 || repo.tagged != "" || out.Len() != 0 {
			t.Fatalf("err=%v inspections=%d out=%s", err, repo.inspections, &out)
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "CHANGELOG.md", "# changes\n")
	writeFile(t, dir, "commit-msg.txt", "chore(release): bump to v1.2.3\n")

	repo := &releaseBoundaryRepo{fakeChangelogReleaseRepo: fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}, remoteExists: true}

	_, err := appversion.ChangelogRelease(t.Context(), repo, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", GitURL: "ssh://git@forge.example/owner/repo.git", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid", SigningKeyPath: filepath.Join(dir, "fake-key")})
	if !errors.Is(err, errs.ErrValidation) || len(repo.cfg) != 0 || len(repo.added) != 0 || repo.pushedBranch != "" {
		t.Fatalf("tag conflict caused mutation: err=%v repo=%+v", err, repo)
	}
}

func TestRecoveryBoundary_ObservedAndFetchedCommitMustAgree(t *testing.T) {
	for _, observed := range []string{"0123456789abcdef0123456789abcdef01234567", strings.Repeat("a", 40), "", "bad"} {
		dir := t.TempDir()
		t.Chdir(dir)

		repo := &fakeChangelogRenderGit{remoteTagExists: true, remoteTagCommit: observed, subject: "chore(release): bump to v1.2.3", ancestor: true}
		renderer := &fakeChangelogRenderer{}

		var out bytes.Buffer

		_, err := appversion.ChangelogRender(t.Context(), repo, renderer, &out, appversion.ChangelogRenderInput{Tag: "v1.2.3", RepositoryURL: "https://forge.example/owner/repo", ChangelogConfig: "full", CommitBodyConfig: "body"})
		if observed == "0123456789abcdef0123456789abcdef01234567" {
			if err != nil || !reflect.DeepEqual(repo.checkedOut, []string{observed}) || readTestFile(t, ".existing-release-sha") != observed+"\n" {
				t.Fatalf("err=%v checkouts=%v", err, repo.checkedOut)
			}
		} else {
			if !errors.Is(err, errs.ErrValidation) || len(repo.checkedOut) != 0 || renderer.fullTag != "" || out.Len() != 0 {
				t.Fatalf("err=%v checkouts=%v renderer=%+v out=%s", err, repo.checkedOut, renderer, &out)
			}

			if _, err := os.Stat(".existing-release-sha"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("premature recovery marker")
			}
		}
	}
}

func TestCommitPreviewBoundary_ZeroMutationsAndLocalAuthority(t *testing.T) {
	t.Parallel()

	for _, dry := range []bool{false, true} {
		repo := &fakeCommitPushRepo{hasStaged: true, localSHA: strings.Repeat("a", 40), remoteSHA: strings.Repeat("a", 40), remoteSeen: true}

		in := appversion.CommitPushInput{Branch: "main", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid", Message: "release", FilePattern: "CHANGELOG.md package.json", ExpectedSHA: strings.Repeat("a", 40), DryRun: dry}
		if err := appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in); err != nil {
			t.Fatal(err)
		}

		if dry {
			if len(repo.added) != 0 || len(repo.cfg) != 0 || repo.committed || repo.pushedRef != "" || !reflect.DeepEqual(repo.statusPaths, []string{"CHANGELOG.md", "package.json"}) {
				t.Fatal("preview mutated or skipped status inspection")
			}
		} else if !repo.committed || repo.pushedRef != strings.Repeat("c", 40) || repo.leasedPush.ExpectedSHA != in.ExpectedSHA || !reflect.DeepEqual(repo.added, []string{"CHANGELOG.md", "package.json"}) {
			t.Fatal("wet control did not commit requested paths")
		}

		repo = &fakeCommitPushRepo{hasStaged: true, localSHA: strings.Repeat("b", 40), remoteSHA: in.ExpectedSHA, remoteSeen: true}

		err := appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, in)
		if !errors.Is(err, errs.ErrValidation) || len(repo.added) != 0 || len(repo.cfg) != 0 || repo.committed {
			t.Fatalf("local mismatch err=%v repo=%+v", err, repo)
		}
	}
}

type retryNPM struct {
	err   error
	calls int
}

func (f *retryNPM) RunInherit(context.Context, string, io.Writer, io.Writer, ...string) error {
	f.calls++

	return f.err
}

func TestBumpRetryBoundary_AndroidCodeAndAdditivePaths(t *testing.T) { //nolint:gocognit // independently fail a tool and the sink, then assert two retries converge on the same bytes and path union.
	for _, toolFailure := range []bool{false, true} {
		root := t.TempDir()
		t.Chdir(root)

		for _, dir := range []string{"android", "web"} {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}

		writeFile(t, root, "full.md", "# complete changes\n")
		writeFile(t, root, "android/gradle.properties", "versionName=1.0.0\nversionCode=42\n")
		writeFile(t, root, "web/package.json", `{"name":"owned-web","version":"1.0.0"}`)

		plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", FilePattern: "custom.txt custom.txt", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{{Name: "android", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android"}}}}}
		npm := &retryNPM{}

		if toolFailure {
			plan.Targets.VersionBump.Items = append(plan.Targets.VersionBump.Items, pipeline.PlannedArtifact{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "web"})
			npm.err = errs.ErrDependencyUnavailable
		}

		body, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}

		in := appversion.BumpPlanInput{PlanJSON: string(body), Version: "2.0.0", ChangelogFile: "full.md"}

		sink := fakeoutputsink.New(t)
		if !toolFailure {
			if closeErr := sink.Close(t.Context()); closeErr != nil {
				t.Fatal(closeErr)
			}
		}

		err = appversion.BumpPlan(t.Context(), appversion.BumpOps{NPM: npm}, sink, io.Discard, io.Discard, output.Annotator{}, in)
		if err == nil {
			t.Fatal("failure fixture passed")
		}

		if got := readTestFile(t, "android/gradle.properties"); got != "versionName=2.0.0\nversionCode=43\n" {
			t.Fatalf("failure did not reach the intended post-Android stage: %s", got)
		}

		if toolFailure && (!errors.Is(err, errs.ErrDependencyUnavailable) || npm.calls != 1) {
			t.Fatalf("wrong tool failure: %v calls=%d", err, npm.calls)
		}

		npm.err = nil

		sink = fakeoutputsink.New(t)
		for range 2 {
			if err := appversion.BumpPlan(t.Context(), appversion.BumpOps{NPM: npm}, sink, io.Discard, io.Discard, output.Annotator{}, in); err != nil {
				t.Fatal(err)
			}
		}

		if got := readTestFile(t, "android/gradle.properties"); got != "versionName=2.0.0\nversionCode=43\n" {
			t.Fatalf("retry incremented code again: %s", got)
		}

		pattern := sink.Single("file-pattern")
		if strings.Count(pattern, "custom.txt") != 1 || strings.Count(pattern, "android/CHANGELOG.md") != 1 {
			t.Fatalf("lost/duplicate changelog path: %s", pattern)
		}
	}
}

func TestBumpPlanBoundary_LateLocalRefusalPreservesChangelog(t *testing.T) {
	for _, badType := range []bool{false, true} {
		root := t.TempDir()
		t.Chdir(root)

		if err := os.Mkdir("first", 0o700); err != nil {
			t.Fatal(err)
		}

		writeFile(t, root, "full.md", "new changes\n")
		writeFile(t, root, "first/CHANGELOG.md", "previous changes\n")

		late := pipeline.PlannedArtifact{Name: "late", ProjectType: projecttype.Go, WorkingDirectory: "../outside"}
		if badType {
			late.WorkingDirectory = "first"
			late.ProjectType = "unknown"
		}

		plan := pipeline.ReleasePrepareStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "prepare", Targets: pipeline.ReleasePrepareTargets{VersionBump: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{{Name: "first", ProjectType: projecttype.Go, WorkingDirectory: "first"}, late}}}}

		body, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}

		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		err = appversion.BumpPlan(t.Context(), appversion.BumpOps{}, sink, &out, &out, output.Annotator{}, appversion.BumpPlanInput{PlanJSON: string(body), Version: "2.0.0", ChangelogFile: "full.md"})
		if err == nil || out.Len() != 0 || len(sink.Keys()) != 0 || readTestFile(t, "first/CHANGELOG.md") != "previous changes\n" {
			t.Fatalf("late refusal had effects: %v out=%s", err, &out)
		}
	}
}
