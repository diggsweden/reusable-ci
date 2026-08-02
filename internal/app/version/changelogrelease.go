// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// changelogReleaseOps is the git surface needed to sign/push the release bump
// commit, then create the immutable release tag. It embeds the tag-release
// port so the two use cases share the same create-once semantics.
type changelogReleaseOps interface {
	tagReleaseOps
	Config(ctx context.Context, key, value string) error
	SetRemoteURL(ctx context.Context, remote, url string) error
	StatusPorcelain(ctx context.Context, pathspec string) (string, error)
	AddPathspecsStrict(ctx context.Context, pathspecs []string) error
	Commit(ctx context.Context, in domaingit.CommitInput) error
	PushBranchNoForce(ctx context.Context, branch, token string) error
	Checkout(ctx context.Context, ref string) error
}

// ChangelogReleaseInput drives `reusable-ci version commit-changelog-release`.
type ChangelogReleaseInput struct {
	Tag               string // final stable release tag, e.g. v1.2.3
	Repository        string // owner/name, used only to build the SSH origin URL
	RemoteHost        string // empty defaults to codeberg.org
	RemoteName        string // empty defaults to origin
	Branch            string // empty defaults to main
	ChangelogPath     string // empty defaults to CHANGELOG.md
	CommitMessageFile string // empty defaults to commit-msg.txt
	AuthorName        string // empty defaults to Itiquette Release Bot
	AuthorEmail       string // empty defaults to itiquette-release-bot@pm.me
	SigningKeyPath    string // SSH private key path already written to a temp dir
	TagSigned         bool
	Token             string
}

// ChangelogRelease commits a pre-rendered CHANGELOG.md with SSH git signing,
// pushes the release bump to main without force, creates the final tag once,
// and checks out that tag. Consumer-owned template rendering stays outside this
// function; this owns only the trusted signing/tagging mutation sequence.
func ChangelogRelease(ctx context.Context, repo changelogReleaseOps, sink ci.OutputSink, out io.Writer, in ChangelogReleaseInput) (*TagReleaseOutput, error) {
	if err := validateChangelogReleaseInput(in); err != nil {
		return nil, err
	}

	in = withChangelogReleaseDefaults(in)

	status, err := inspectChangelogReleaseFiles(ctx, repo, in)
	if err != nil {
		return nil, err
	}

	if err = configureChangelogReleaseGit(ctx, repo, in); err != nil {
		return nil, err
	}

	if err = commitChangelogIfChanged(ctx, repo, out, in, status); err != nil {
		return nil, err
	}

	res, err := TagRelease(ctx, repo, TagReleaseInput{Tag: in.Tag, Signed: in.TagSigned, Remote: in.RemoteName, Token: in.Token}, sink, out)
	if err != nil {
		return nil, err
	}

	if err = repo.Checkout(ctx, in.Tag); err != nil {
		return nil, fmt.Errorf("commit-changelog: checkout %s: %w", in.Tag, err)
	}

	return res, nil
}

// commitChangelogIfChanged commits and pushes the changelog when the porcelain
// status reports changes; an unchanged changelog only logs a skip notice.
func commitChangelogIfChanged(ctx context.Context, repo changelogReleaseOps, out io.Writer, in ChangelogReleaseInput, status string) error {
	if strings.TrimSpace(status) == "" {
		_, _ = fmt.Fprintln(out, "CHANGELOG.md unchanged, skipping commit")

		return nil
	}

	if err := requireRegularFile("commit-msg.txt must be generated before loading the SSH signing key", in.CommitMessageFile); err != nil {
		return err
	}

	if err := repo.AddPathspecsStrict(ctx, []string{in.ChangelogPath}); err != nil {
		return fmt.Errorf("commit-changelog: add %s: %w", in.ChangelogPath, err)
	}

	if err := repo.Commit(ctx, domaingit.CommitInput{MessageFile: in.CommitMessageFile, Sign: true, Signoff: true, NoVerify: true, NoHooks: true}); err != nil {
		return fmt.Errorf("commit-changelog: signed commit: %w", err)
	}

	if err := repo.PushBranchNoForce(ctx, in.Branch, in.Token); err != nil {
		return fmt.Errorf("commit-changelog: push %s: %w", in.Branch, err)
	}

	_, _ = fmt.Fprintln(out, "CHANGELOG.md committed")

	return nil
}

// ChangelogReleasePreflight validates non-secret inputs and the changelog file
// before the caller loads SSH_SIGNING_KEY into process memory. It intentionally
// avoids git subprocesses so the secret env var cannot be inherited before the
// CLI has copied it to a temp key file and unset it.
func ChangelogReleasePreflight(in ChangelogReleaseInput) error {
	if in.SigningKeyPath == "" {
		in.SigningKeyPath = "preflight"
	}

	if err := validateChangelogReleaseInput(in); err != nil {
		return err
	}

	in = withChangelogReleaseDefaults(in)

	return requireRegularFile("CHANGELOG.md must be generated before loading the SSH signing key", in.ChangelogPath)
}

func validateChangelogReleaseInput(in ChangelogReleaseInput) error {
	switch {
	case in.Tag == "":
		return fmt.Errorf("commit-changelog: tag is required: %w", errs.ErrUsage)
	case strings.ContainsAny(in.Tag, "\n\r") || !domainversion.IsStableSemverTag(in.Tag):
		return fmt.Errorf("commit-changelog: version tag must look like stable vMAJOR.MINOR.PATCH: %s: %w", in.Tag, errs.ErrValidation)
	case in.Repository == "":
		return fmt.Errorf("commit-changelog: repository is required: %w", errs.ErrUsage)
	case strings.ContainsAny(in.Repository, "\n\r"):
		return fmt.Errorf("commit-changelog: repository must be a single-line owner/name value: %w", errs.ErrValidation)
	case in.SigningKeyPath == "":
		return fmt.Errorf("commit-changelog: signing key path is required: %w", errs.ErrUsage)
	}

	return nil
}

func withChangelogReleaseDefaults(in ChangelogReleaseInput) ChangelogReleaseInput {
	if in.RemoteHost == "" {
		in.RemoteHost = "codeberg.org"
	}

	if in.RemoteName == "" {
		in.RemoteName = defaultRemoteName
	}

	if in.Branch == "" {
		in.Branch = "main"
	}

	if in.ChangelogPath == "" {
		in.ChangelogPath = "CHANGELOG.md"
	}

	if in.CommitMessageFile == "" {
		in.CommitMessageFile = "commit-msg.txt"
	}

	if in.AuthorName == "" {
		in.AuthorName = "Itiquette Release Bot"
	}

	if in.AuthorEmail == "" {
		in.AuthorEmail = "itiquette-release-bot@pm.me"
	}

	return in
}

func configureChangelogReleaseGit(ctx context.Context, repo changelogReleaseOps, in ChangelogReleaseInput) error {
	pairs := [][2]string{
		{"user.name", in.AuthorName},
		{"user.email", in.AuthorEmail},
		{"gpg.format", "ssh"},
		{"user.signingkey", in.SigningKeyPath},
		{"commit.gpgsign", "true"},
	}
	for _, pair := range pairs {
		if err := repo.Config(ctx, pair[0], pair[1]); err != nil {
			return fmt.Errorf("commit-changelog: set %s: %w", pair[0], err)
		}
	}

	remoteURL := fmt.Sprintf("git@%s:%s.git", in.RemoteHost, in.Repository)
	if err := repo.SetRemoteURL(ctx, in.RemoteName, remoteURL); err != nil {
		return fmt.Errorf("commit-changelog: set %s URL: %w", in.RemoteName, err)
	}

	return nil
}

func inspectChangelogReleaseFiles(ctx context.Context, repo interface {
	StatusPorcelain(ctx context.Context, pathspec string) (string, error)
}, in ChangelogReleaseInput) (string, error) {
	if err := requireRegularFile("CHANGELOG.md must be generated before loading the SSH signing key", in.ChangelogPath); err != nil {
		return "", err
	}

	status, err := repo.StatusPorcelain(ctx, in.ChangelogPath)
	if err != nil {
		return "", fmt.Errorf("commit-changelog: inspect %s: %w", in.ChangelogPath, err)
	}

	if strings.TrimSpace(status) != "" {
		if err := requireRegularFile("commit-msg.txt must be generated before loading the SSH signing key", in.CommitMessageFile); err != nil {
			return "", err
		}
	}

	return status, nil
}

func requireRegularFile(message, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("commit-changelog: %s: %w", message, errs.ErrMissingInput)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("commit-changelog: %s: %s is not a regular file: %w", message, path, errs.ErrValidation)
	}

	return nil
}
