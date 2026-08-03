// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type fakeChangelogRenderGit struct {
	remoteTagCommit string
	remoteTagExists bool
	subject         string
	ancestor        bool
	recentLog       string
	status          string

	fetchedBranch bool
	checkedOut    []string
	fetchedTag    string
}

func (f *fakeChangelogRenderGit) FetchBranch(_ context.Context, _, _ string) error {
	f.fetchedBranch = true

	return nil
}

func (f *fakeChangelogRenderGit) Checkout(_ context.Context, ref string) error {
	f.checkedOut = append(f.checkedOut, ref)

	return nil
}

func (f *fakeChangelogRenderGit) RemoteTagCommitIfExists(_ context.Context, _, _ string) (string, bool, error) {
	return f.remoteTagCommit, f.remoteTagExists, nil
}

func (f *fakeChangelogRenderGit) FetchTagForceFromRemote(_ context.Context, _, tag string) error {
	f.fetchedTag = tag

	return nil
}

func (f *fakeChangelogRenderGit) RevParse(_ context.Context, _ string) (string, error) {
	return "0123456789abcdef0123456789abcdef01234567", nil
}

func (f *fakeChangelogRenderGit) CommitSubject(_ context.Context, _ string) (string, error) {
	return f.subject, nil
}

func (f *fakeChangelogRenderGit) IsAncestor(_ context.Context, _, _ string) (bool, error) {
	return f.ancestor, nil
}

func (f *fakeChangelogRenderGit) RecentLogOneline(_ context.Context, _ string, _ int) (string, error) {
	return f.recentLog, nil
}

func (f *fakeChangelogRenderGit) StatusPorcelain(_ context.Context, _ string) (string, error) {
	return f.status, nil
}

type fakeChangelogRenderer struct {
	backend string
	fullTag string
	bodyTag string
}

func (f *fakeChangelogRenderer) RenderFull(_ context.Context, backend, _, tag, outputPath string) error {
	f.backend = backend
	f.fullTag = tag

	return os.WriteFile(outputPath, []byte("# Release\n\nChanges\n"), 0o600)
}

func (f *fakeChangelogRenderer) RenderBody(_ context.Context, _, _, tag string) (string, error) {
	f.bodyTag = tag

	return "body line\n", nil
}

func TestChangelogRender_GitChglogRendersCommitMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	gitr := &fakeChangelogRenderGit{status: " M CHANGELOG.md"}
	renderer := &fakeChangelogRenderer{}

	var out bytes.Buffer

	res, err := appversion.ChangelogRender(context.Background(), gitr, renderer, &out, appversion.ChangelogRenderInput{
		Backend:          "git-chglog",
		Tag:              "v1.2.3",
		ChangelogConfig:  ".chglog/full.yml",
		CommitBodyConfig: ".chglog/body.yml",
		CommitTrailers:   "Release-Request: release-request/v1.2.3",
	})
	if err != nil {
		t.Fatalf("ChangelogRender: %v", err)
	}

	if !res.Rendered || renderer.backend != "git-chglog" || renderer.fullTag != "v1.2.3" || renderer.bodyTag != "v1.2.3" {
		t.Fatalf("unexpected render result: res=%+v renderer=%+v", res, renderer)
	}

	msg := readTestFile(t, "commit-msg.txt")
	for _, want := range []string{"chore(release): bump to v1.2.3", "body line", "[skip ci]", "Release-Request: release-request/v1.2.3"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("commit-msg.txt missing %q:\n%s", want, msg)
		}
	}

	if !strings.Contains(out.String(), "Generated CHANGELOG.md (3 lines)") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestChangelogRender_ExistingReleaseRecoverySkipsRenderer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	gitr := &fakeChangelogRenderGit{remoteTagExists: true, remoteTagCommit: "remote", subject: "chore(release): bump to v1.2.3", ancestor: true}
	renderer := &fakeChangelogRenderer{}

	res, err := appversion.ChangelogRender(context.Background(), gitr, renderer, &bytes.Buffer{}, appversion.ChangelogRenderInput{
		Tag:              "v1.2.3",
		ChangelogConfig:  "full",
		CommitBodyConfig: "body",
	})
	if err != nil {
		t.Fatalf("ChangelogRender: %v", err)
	}

	if res.ExistingReleaseSHA == "" || renderer.fullTag != "" || gitr.fetchedTag != "v1.2.3" {
		t.Fatalf("recovery result mismatch: res=%+v renderer=%+v git=%+v", res, renderer, gitr)
	}

	if got := readTestFile(t, ".existing-release-sha"); !strings.Contains(got, res.ExistingReleaseSHA) {
		t.Fatalf("existing sha marker = %q, want %q", got, res.ExistingReleaseSHA)
	}
}

func TestChangelogRender_RejectsInvalidBackend(t *testing.T) {
	t.Parallel()

	_, err := appversion.ChangelogRender(context.Background(), &fakeChangelogRenderGit{}, &fakeChangelogRenderer{}, &bytes.Buffer{}, appversion.ChangelogRenderInput{
		Backend:          "bad",
		Tag:              "v1.2.3",
		ChangelogConfig:  "full",
		CommitBodyConfig: "body",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func readTestFile(t *testing.T, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Clean(rel))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
