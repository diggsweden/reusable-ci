// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// backendGitChglog is the default changelog renderer backend.
const backendGitChglog = "git-chglog"

type changelogRenderGit interface {
	FetchBranch(ctx context.Context, remote, branch string, cred runcontext.Credential) error
	Checkout(ctx context.Context, ref string) error
	RemoteTagCommitIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (commit string, exists bool, err error)
	FetchTagForceFromRemote(ctx context.Context, remote, tag string, cred runcontext.Credential) error
	RevParse(ctx context.Context, ref string) (string, error)
	CommitSubject(ctx context.Context, commit string) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	RecentLogOneline(ctx context.Context, ref string, limit int) (string, error)
	StatusPorcelain(ctx context.Context, pathspec string) (string, error)
}

type changelogRenderer interface {
	RenderFull(ctx context.Context, backend, config, tag, outputPath string) error
	RenderBody(ctx context.Context, backend, config, tag string) (string, error)
}

// ChangelogRenderInput drives `reusable-ci version render-changelog`.
type ChangelogRenderInput struct {
	Backend                string // git-chglog or git-cliff; empty defaults to git-chglog
	Tag                    string // final stable release tag
	Remote                 string // empty defaults to origin
	Branch                 string // empty defaults to main
	ChangelogConfig        string
	CommitBodyConfig       string
	ChangelogPath          string // empty defaults to CHANGELOG.md
	CommitBodyPath         string // empty defaults to commit-body.txt
	CommitMessagePath      string // empty defaults to commit-msg.txt
	ExistingReleaseSHAPath string // empty defaults to .existing-release-sha
	CommitTrailers         string
	Token                  runcontext.Credential
}

// ChangelogRenderOutput reports whether rendering occurred or same-version
// recovery reused an existing final release tag.
type ChangelogRenderOutput struct {
	ExistingReleaseSHA string
	Rendered           bool
}

// ChangelogRender renders CHANGELOG.md and commit-msg.txt before signing-key
// material is loaded. It preserves forgejo-ci's same-version recovery behavior:
// if the final tag already exists and points at the expected release bump commit
// on origin/main, it writes the recovery SHA marker and checks out that commit.
func ChangelogRender(ctx context.Context, repo changelogRenderGit, renderer changelogRenderer, out io.Writer, in ChangelogRenderInput) (*ChangelogRenderOutput, error) {
	if err := validateChangelogRenderInput(in); err != nil {
		return nil, err
	}

	in = withChangelogRenderDefaults(in)

	if err := repo.FetchBranch(ctx, in.Remote, in.Branch, in.Token); err != nil {
		return nil, fmt.Errorf("render-changelog: fetch %s %s: %w", in.Remote, in.Branch, err)
	}

	if err := repo.Checkout(ctx, in.Branch); err != nil {
		return nil, fmt.Errorf("render-changelog: checkout %s: %w", in.Branch, err)
	}

	reused, err := tryExistingReleaseRecovery(ctx, repo, out, in)
	if err != nil {
		return nil, err
	}

	if reused != "" {
		return &ChangelogRenderOutput{ExistingReleaseSHA: reused}, nil
	}

	if err = warnExistingBump(ctx, repo, out, in); err != nil {
		return nil, err
	}

	if err = renderChangelogFull(ctx, renderer, out, in); err != nil {
		return nil, err
	}

	if err = finalizeChangelogCommitFiles(ctx, repo, renderer, out, in); err != nil {
		return nil, err
	}

	return &ChangelogRenderOutput{Rendered: true}, nil
}

// finalizeChangelogCommitFiles writes the commit-message files when the
// rendered changelog actually changed; an unchanged changelog needs no
// release bump commit, so it is a successful no-op.
func finalizeChangelogCommitFiles(ctx context.Context, repo changelogRenderGit, renderer changelogRenderer, out io.Writer, in ChangelogRenderInput) error {
	status, err := repo.StatusPorcelain(ctx, in.ChangelogPath)
	if err != nil {
		return fmt.Errorf("render-changelog: inspect %s: %w", in.ChangelogPath, err)
	}

	if strings.TrimSpace(status) == "" {
		_, _ = fmt.Fprintf(out, "%s unchanged; no release bump commit needed\n", in.ChangelogPath)

		return nil
	}

	return writeCommitMessageFiles(ctx, renderer, in)
}

// renderChangelogFull renders the full changelog file and reports its size.
func renderChangelogFull(ctx context.Context, renderer changelogRenderer, out io.Writer, in ChangelogRenderInput) error {
	if err := renderer.RenderFull(ctx, in.Backend, in.ChangelogConfig, in.Tag, in.ChangelogPath); err != nil {
		return fmt.Errorf("render-changelog: render %s with %s: %w", in.ChangelogPath, in.Backend, err)
	}

	lineCount, err := countFileLines(in.ChangelogPath)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Generated %s (%d lines)\n", in.ChangelogPath, lineCount)

	return nil
}

// writeCommitMessageFiles renders the commit body and writes the commit-body
// and commit-message files consumed by the later signing step.
func writeCommitMessageFiles(ctx context.Context, renderer changelogRenderer, in ChangelogRenderInput) error {
	body, err := renderer.RenderBody(ctx, in.Backend, in.CommitBodyConfig, in.Tag)
	if err != nil {
		return fmt.Errorf("render-changelog: render commit body with %s: %w", in.Backend, err)
	}

	if err := os.WriteFile(in.CommitBodyPath, []byte(body), 0o644); err != nil { //nolint:gosec // non-secret commit body consumed by later CI steps.
		return fmt.Errorf("render-changelog: write %s: %w", in.CommitBodyPath, err)
	}

	if err := os.WriteFile(in.CommitMessagePath, []byte(commitMessage(in.Tag, body, in.CommitTrailers)), 0o644); err != nil { //nolint:gosec // non-secret commit message consumed by later CI steps.
		return fmt.Errorf("render-changelog: write %s: %w", in.CommitMessagePath, err)
	}

	return nil
}

func validateChangelogRenderInput(in ChangelogRenderInput) error {
	switch {
	case in.Tag == "":
		return fmt.Errorf("render-changelog: tag is required: %w", errs.ErrUsage)
	case strings.ContainsAny(in.Tag, "\n\r") || !domainversion.IsStableSemverTag(in.Tag):
		return fmt.Errorf("render-changelog: tag must look like stable vMAJOR.MINOR.PATCH: %s: %w", in.Tag, errs.ErrValidation)
	case in.ChangelogConfig == "":
		return fmt.Errorf("render-changelog: changelog config is required: %w", errs.ErrUsage)
	case in.CommitBodyConfig == "":
		return fmt.Errorf("render-changelog: commit body config is required: %w", errs.ErrUsage)
	}

	backend := in.Backend
	if backend == "" {
		backend = backendGitChglog
	}

	if backend != backendGitChglog && backend != "git-cliff" {
		return fmt.Errorf("render-changelog: backend must be git-chglog or git-cliff: %s: %w", backend, errs.ErrValidation)
	}

	return nil
}

func withChangelogRenderDefaults(in ChangelogRenderInput) ChangelogRenderInput {
	if in.Backend == "" {
		in.Backend = backendGitChglog
	}

	if in.Remote == "" {
		in.Remote = defaultRemoteName
	}

	if in.Branch == "" {
		in.Branch = "main"
	}

	if in.ChangelogPath == "" {
		in.ChangelogPath = "CHANGELOG.md"
	}

	if in.CommitBodyPath == "" {
		in.CommitBodyPath = "commit-body.txt"
	}

	if in.CommitMessagePath == "" {
		in.CommitMessagePath = "commit-msg.txt"
	}

	if in.ExistingReleaseSHAPath == "" {
		in.ExistingReleaseSHAPath = ".existing-release-sha"
	}

	return in
}

func tryExistingReleaseRecovery(ctx context.Context, repo changelogRenderGit, out io.Writer, in ChangelogRenderInput) (string, error) {
	remoteCommit, exists, err := repo.RemoteTagCommitIfExists(ctx, in.Remote, in.Tag, in.Token)
	if err != nil {
		return "", fmt.Errorf("render-changelog: resolve remote tag %s: %w", in.Tag, err)
	}

	if !exists {
		return "", nil
	}

	_ = remoteCommit

	if err = repo.FetchTagForceFromRemote(ctx, in.Remote, in.Tag, in.Token); err != nil {
		return "", fmt.Errorf("render-changelog: fetch existing release tag %s: %w", in.Tag, err)
	}

	sha, err := repo.RevParse(ctx, in.Tag+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("render-changelog: resolve existing release tag commit: %w", err)
	}

	if err = verifyExistingReleaseCommit(ctx, repo, in, sha); err != nil {
		return "", err
	}

	if err = os.WriteFile(in.ExistingReleaseSHAPath, []byte(sha+"\n"), 0o644); err != nil { //nolint:gosec // non-secret recovery marker consumed by later CI steps.
		return "", fmt.Errorf("render-changelog: write %s: %w", in.ExistingReleaseSHAPath, err)
	}

	if err = repo.Checkout(ctx, sha); err != nil {
		return "", fmt.Errorf("render-changelog: checkout existing release commit: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Existing final release tag detected for %s; reusing %s for same-version recovery.\n", in.Tag, sha)

	return sha, nil
}

// verifyExistingReleaseCommit checks that sha is the release bump commit for
// in.Tag and that it sits on <remote>/<branch> history before recovery reuses it.
func verifyExistingReleaseCommit(ctx context.Context, repo changelogRenderGit, in ChangelogRenderInput, sha string) error {
	subject, err := repo.CommitSubject(ctx, sha)
	if err != nil {
		return fmt.Errorf("render-changelog: inspect existing release commit subject: %w", err)
	}

	expectedSubject := "chore(release): bump to " + in.Tag
	if subject != expectedSubject {
		return fmt.Errorf("render-changelog: existing final release tag %s does not point at its release bump commit: %w", in.Tag, errs.ErrValidation)
	}

	ancestor, err := repo.IsAncestor(ctx, sha, in.Remote+"/"+in.Branch)
	if err != nil {
		return fmt.Errorf("render-changelog: check existing release commit ancestry: %w", err)
	}

	if !ancestor {
		return fmt.Errorf("render-changelog: existing final release tag %s does not point at %s/%s history: %w", in.Tag, in.Remote, in.Branch, errs.ErrValidation)
	}

	return nil
}

func warnExistingBump(ctx context.Context, repo changelogRenderGit, out io.Writer, in ChangelogRenderInput) error {
	logOut, err := repo.RecentLogOneline(ctx, in.Remote+"/"+in.Branch, 20)
	if err != nil {
		return fmt.Errorf("render-changelog: inspect recent %s/%s commits: %w", in.Remote, in.Branch, err)
	}

	if strings.Contains(logOut, "chore(release): bump to "+in.Tag) {
		_, _ = fmt.Fprintf(out, "Existing release bump detected for %s; continuing for same-version recovery.\n", in.Tag)
	}

	return nil
}

func countFileLines(path string) (int, error) {
	body, err := os.ReadFile(path) //nolint:gosec // caller-configured changelog path.
	if err != nil {
		return 0, fmt.Errorf("render-changelog: read %s: %w", path, err)
	}

	if len(body) == 0 {
		return 0, nil
	}

	return strings.Count(string(body), "\n"), nil
}

func commitMessage(tag, body, trailers string) string {
	msg := fmt.Sprintf("chore(release): bump to %s\n\n%s\n[skip ci]\n", tag, body)
	if trailers != "" {
		msg += "\n" + trailers + "\n"
	}

	return msg
}
