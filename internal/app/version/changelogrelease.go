// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
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
	PushBranchNoForce(ctx context.Context, branch string, cred runcontext.Credential) error
	Checkout(ctx context.Context, ref string) error
}

// ChangelogReleaseInput drives `reusable-ci version commit-changelog-release`.
type ChangelogReleaseInput struct {
	Tag               string // final stable release tag, e.g. v1.2.3
	Repository        string // owner/name, validated against the SSH origin URL path
	GitURL            string // required: exact ssh://git@host[:port]/owner/name.git origin URL
	RemoteName        string // empty defaults to origin
	Branch            string // empty defaults to main
	ChangelogPath     string // empty defaults to CHANGELOG.md
	CommitMessageFile string // empty defaults to commit-msg.txt
	AuthorName        string // required: git user.name for the release commit (no org default)
	AuthorEmail       string // required: git user.email for the release commit (no org default)
	SigningKeyPath    string // SSH private key path already written to a temp dir; optional in dry-run (the signing setup only serves the skipped push)
	TagSigned         bool
	Token             runcontext.Credential
	DryRun            bool // preview: narrate the git config/commit/push/tag/checkout mutations instead of performing them
}

// ChangelogRelease commits a pre-rendered CHANGELOG.md with SSH git signing,
// pushes the release bump to main without force, creates the final tag once,
// and checks out that tag. Consumer-owned template rendering stays outside this
// function; this owns only the trusted signing/tagging mutation sequence.
//
// DryRun keeps every validation and the changelog status inspection, then
// narrates and skips each git mutation (signing/remote config, stage, commit,
// push, tag create/push, checkout).
func ChangelogRelease(ctx context.Context, repo changelogReleaseOps, sink ci.OutputSink, out io.Writer, in ChangelogReleaseInput) (*TagReleaseOutput, error) {
	if err := validateChangelogReleaseInput(in); err != nil {
		return nil, err
	}

	in = withChangelogReleaseDefaults(in)

	status, err := inspectChangelogReleaseFiles(ctx, repo, in)
	if err != nil {
		return nil, err
	}

	if in.DryRun {
		// The signing/remote git config only serves the commit and push that
		// dry-run skips — leave the checkout's config and remote untouched.
		_, _ = fmt.Fprintln(out, "[dry-run] skipping git signing config and SSH remote setup (commit and push are skipped)")
	} else if err = configureChangelogReleaseGit(ctx, repo, in); err != nil {
		return nil, err
	}

	if err = commitChangelogIfChanged(ctx, repo, out, in, status); err != nil {
		return nil, err
	}

	res, err := TagRelease(ctx, repo, TagReleaseInput{Tag: in.Tag, Signed: in.TagSigned, Remote: in.RemoteName, Token: in.Token, DryRun: in.DryRun}, sink, out)
	if err != nil {
		return nil, err
	}

	if in.DryRun {
		_, _ = fmt.Fprintf(out, "[dry-run] would checkout %s\n", in.Tag)

		return res, nil
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

	if in.DryRun {
		_, _ = fmt.Fprintf(out, "[dry-run] would stage %s\n", in.ChangelogPath)
		_, _ = fmt.Fprintf(out, "[dry-run] would create the signed changelog commit from %s\n", in.CommitMessageFile)
		_, _ = fmt.Fprintf(out, "[dry-run] would push %s to %s (no force)\n", in.Branch, in.RemoteName)

		return nil
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
	case in.SigningKeyPath == "" && !in.DryRun:
		// The signing key only serves the commit/push that dry-run skips,
		// so a preview may run without one.
		return fmt.Errorf("commit-changelog: signing key path is required: %w", errs.ErrUsage)
	}

	// Org identity has no default: a three-forge engine must not assume one
	// forge's host or ship one org's release identity. Each must be supplied
	// by the caller/shell, and we fail loudly (with the flag + env) when it is
	// not — see the Tier A coherence track and ADR-0001.
	required := []struct{ name, value, flag, env string }{
		{"git URL", in.GitURL, "--git-url", "$RELEASE_GIT_URL"},
		{"author name", in.AuthorName, "--author-name", "$GIT_USER_NAME"},
		{"author email", in.AuthorEmail, "--author-email", "$GIT_USER_EMAIL"},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("commit-changelog: %s is required (set %s or %s): %w", field.name, field.flag, field.env, errs.ErrUsage)
		}
	}

	if _, err := ParseChangelogReleaseGitURL(in.GitURL, in.Repository); err != nil {
		return err
	}

	return nil
}

// ChangelogReleaseGitEndpoint is the host and effective port extracted from a
// validated commit-changelog-release SSH URL.
type ChangelogReleaseGitEndpoint struct {
	Host string
	Port int
}

// ParseChangelogReleaseGitURL validates the pre-release SSH URL contract and
// returns the connection endpoint used for host-key pinning.
func ParseChangelogReleaseGitURL(rawURL, repository string) (ChangelogReleaseGitEndpoint, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." ||
		strings.ContainsAny(repository, "\\%?#") || url.PathEscape(parts[0]) != parts[0] || url.PathEscape(parts[1]) != parts[1] {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: repository must be an unambiguous owner/name value: %w", errs.ErrValidation)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: invalid git URL: %w: %w", err, errs.ErrValidation)
	}

	if parsed.Scheme != "ssh" || parsed.Opaque != "" {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL must use the ssh scheme: %w", errs.ErrValidation)
	}

	if parsed.User == nil || parsed.User.String() != "git" {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL user must be exactly git with no password: %w", errs.ErrValidation)
	}

	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(rawURL, "?#") {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL must not contain a query or fragment: %w", errs.ErrValidation)
	}

	host := parsed.Hostname()
	if host == "" {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL hostname is required: %w", errs.ErrValidation)
	}

	port := 22
	portText := parsed.Port()
	if portText == "" {
		if strings.HasSuffix(parsed.Host, ":") {
			return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL port must be numeric from 1 to 65535: %w", errs.ErrValidation)
		}
	} else {
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL port must be numeric from 1 to 65535: %w", errs.ErrValidation)
		}
	}

	expected := "ssh://git@" + parsed.Host + "/" + repository + ".git"
	if rawURL != expected || parsed.Path != "/"+repository+".git" {
		return ChangelogReleaseGitEndpoint{}, fmt.Errorf("commit-changelog: git URL must be exactly ssh://git@host[:port]/%s.git: %w", repository, errs.ErrValidation)
	}

	return ChangelogReleaseGitEndpoint{Host: host, Port: port}, nil
}

// withChangelogReleaseDefaults fills only the forge-neutral operational
// defaults. Org identity (git URL, author name/email) has no default and is
// required by validateChangelogReleaseInput — a general engine must not ship
// one org's release identity or SSH endpoint.
func withChangelogReleaseDefaults(in ChangelogReleaseInput) ChangelogReleaseInput {
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

	if err := repo.SetRemoteURL(ctx, in.RemoteName, in.GitURL); err != nil {
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
