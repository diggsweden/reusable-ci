// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	// No default remote: the engine must not assume one org's forgejo-ci
	// fork. The caller supplies --remote (or a local --repo-dir).
	defaultPinMain    = "main"
	defaultPinSubject = "forgejo-ci"
)

// PinReachabilityInput drives `validate pin-reachability`.
type PinReachabilityInput struct {
	Workflows []string
	Remote    string
	RepoDir   string
	Main      string
	Subject   string
	GitBin    string
	TempDir   string
}

// PinReachability fails when any pinned forgejo-ci SHA in the workflow files is
// not reachable from the configured main branch and is not the commit pointed to
// by any tag. This catches history-rewrite/orphaned-pin failures while the git
// object may still be resolvable before server-side GC.
func PinReachability(ctx context.Context, out io.Writer, in PinReachabilityInput) error {
	if len(in.Workflows) == 0 {
		return fmt.Errorf("usage: validate pin-reachability --workflow WORKFLOW.yml [--workflow WORKFLOW.yml ...]: %w", errs.ErrUsage)
	}

	mainBranch := defaultString(in.Main, defaultPinMain)
	subject := defaultString(in.Subject, defaultPinSubject)

	pins, err := collectPinReachabilitySHAs(in.Workflows, subject)
	if err != nil {
		return err
	}

	if len(pins) == 0 {
		_, _ = fmt.Fprintf(out, "No %s pins found in the given files.\n", subject)
		_, _ = fmt.Fprintf(out, "%s No %s pins to check.\n", clicolor.Check(out), subject)

		return nil
	}

	repoDir := in.RepoDir
	if repoDir == "" {
		if strings.TrimSpace(in.Remote) == "" {
			return fmt.Errorf("validate pin-reachability: provide --remote (the %s upstream to clone) or --repo-dir (a local clone): %w", subject, errs.ErrUsage)
		}

		repoDir, err = clonePinReachabilityRepo(ctx, in.GitBin, in.Remote, in.TempDir)
		if err != nil {
			return err
		}
	}

	repo := &adaptergit.Repo{Dir: repoDir, GitBin: in.GitBin}

	mainRef := "origin/" + mainBranch
	if _, revErr := repo.RevParse(ctx, mainRef+"^{commit}"); revErr != nil {
		mainRef = mainBranch
	}

	_, _ = fmt.Fprintf(out, "Checking %s pin reachability against %s (%d distinct pin(s))\n\n", subject, mainRef, len(pins))

	failures, err := reportPinReachability(ctx, out, repo, pins, subject, mainRef)
	if err != nil {
		return err
	}

	if failures > 0 {
		return fmt.Errorf("%d orphaned %s pin(s): %w", failures, subject, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "\n%s All %s pins are reachable.\n", clicolor.Check(out), subject)

	return nil
}

// reportPinReachability prints the per-pin reachability verdicts and returns
// the number of orphaned pins.
func reportPinReachability(ctx context.Context, out io.Writer, repo *adaptergit.Repo, pins []string, subject, mainRef string) (int, error) {
	failures := 0

	for _, sha := range pins {
		reachable, err := pinReachable(ctx, repo, sha, mainRef)
		if err != nil {
			return failures, err
		}

		if reachable {
			_, _ = fmt.Fprintf(out, "%s reachable: %s@%s\n", clicolor.Check(out), subject, sha)

			continue
		}

		failures++
		_, _ = fmt.Fprintf(out, "%s ORPHANED: %s@%s is not reachable from %s nor any tag; re-pin to a current commit/tag.\n", clicolor.Cross(out), subject, sha, mainRef)
	}

	return failures, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func collectPinReachabilitySHAs(files []string, subject string) ([]string, error) {
	pattern := regexp.MustCompile(regexp.QuoteMeta(subject) + `[^@ "'\t\r\n]*@([0-9a-f]{40})`)
	seen := map[string]bool{}

	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // operator-supplied workflow path.
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}

		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			if len(match) == 2 {
				seen[match[1]] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for sha := range seen {
		out = append(out, sha)
	}

	sort.Strings(out)

	return out, nil
}

func clonePinReachabilityRepo(ctx context.Context, gitBin, remote, tempDir string) (string, error) {
	if tempDir == "" {
		tempDir = os.Getenv("RUNNER_TEMP")
	}

	if tempDir == "" {
		tempDir = os.TempDir()
	}

	dir, err := os.MkdirTemp(tempDir, "reusable-ci-pins.*")
	if err != nil {
		return "", fmt.Errorf("create temporary clone dir: %w", err)
	}

	runner := &adaptergit.Repo{GitBin: gitBin}
	if _, err := runner.Run(ctx, "clone", "--quiet", "--filter=blob:none", "--no-checkout", remote, dir); err != nil {
		return "", fmt.Errorf("clone %s to check pin reachability: %w", remote, err)
	}

	return dir, nil
}

func pinReachable(ctx context.Context, repo *adaptergit.Repo, sha, mainRef string) (bool, error) {
	if _, err := repo.RevParse(ctx, sha+"^{commit}"); err != nil {
		return false, nil //nolint:nilerr // an unresolvable SHA is simply "not reachable", not a failure.
	}

	ancestor, err := repo.IsAncestor(ctx, sha, mainRef)
	if err == nil && ancestor {
		return true, nil
	}

	tags, err := repo.ListTags(ctx, "")
	if err != nil {
		return false, fmt.Errorf("list tags: %w", err)
	}

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		tagSHA, err := repo.TagSHA(ctx, tag)
		if err != nil {
			return false, fmt.Errorf("resolve tag %s: %w", tag, err)
		}

		if tagSHA == sha {
			return true, nil
		}
	}

	return false, nil
}
