// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// TrivyOps abstracts the trivy adapter for dependency injection.
type TrivyOps interface {
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error)
}

// GitOps is the subset of *git.Repo methods needed by ScanDependencies.
type GitOps interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// ScanDependenciesInput drives ScanDependencies.
type ScanDependenciesInput struct {
	FailOnSeverity string            // default "critical"
	ScanMode       security.ScanMode // empty → ScanModeDiff
	ScanPath       string            // default "."
	// BaseRef is the PR base ref (e.g. "main"). Required for diff
	// mode; empty falls through to full scan with a warning.
	BaseRef string

	// SARIFFile is the path to write the trivy-converted SARIF.
	// Default: "trivy-dependency-results.sarif".
	SARIFFile string
	// GitLabDepFile is the path to write the GitLab dep-scanning report.
	// Default: "gl-dependency-scanning-report.json".
	GitLabDepFile string
	// TrivyVersion is embedded into the GitLab report metadata
	// (mirrors the existing trivy-to-gitlab-dep flag).
	TrivyVersion string
}

// ScanDependencies runs a Trivy filesystem scan against HEAD and, in
// diff mode with a base ref, against the base via `git worktree`. New
// vulnerabilities are the set in HEAD but not in base. Mirrors
// scripts/security/scan-dependencies.sh.
//
// Returns an error when new vulnerabilities are found at or above the
// requested severity threshold.
func ScanDependencies(
	ctx context.Context,
	trivy TrivyOps,
	gitRepo GitOps,
	summary ci.SummarySink,
	stdout, stderr io.Writer,
	annot output.Annotator,
	in ScanDependenciesInput,
) error {
	failOnSev := cmp.Or(in.FailOnSeverity, "critical")
	mode := in.ScanMode
	if mode == "" {
		mode = security.ScanModeDiff
	}
	scanPath := cmp.Or(in.ScanPath, ".")
	sarifFile := cmp.Or(in.SARIFFile, security.DefaultTrivySARIFFile)
	glFile := cmp.Or(in.GitLabDepFile, security.DefaultTrivyGitLabDepFile)

	fmt.Fprintf(stdout, "🔍 Dependency vulnerability scan\n")
	fmt.Fprintf(stdout, "   Severity threshold: %s\n", failOnSev)
	fmt.Fprintf(stdout, "   Scan mode: %s\n", mode)
	fmt.Fprintf(stdout, "   Scan path: %s\n\n", scanPath)

	severityFilter := security.MapTrivyFailSeverity(failOnSev)
	if in.FailOnSeverity != "" && !security.IsKnownTrivyFailSeverity(in.FailOnSeverity) {
		annot.Warningf("Unknown severity %q, defaulting to critical", in.FailOnSeverity)
	}

	workDir, err := os.MkdirTemp("", "scan-deps-")
	if err != nil {
		return fmt.Errorf("mkdir tmp: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	headJSON := filepath.Join(workDir, "head-vulns.json")
	baseJSON := filepath.Join(workDir, "base-vulns.json")

	// Scan HEAD.
	fmt.Fprintln(stdout, "Scanning HEAD for vulnerabilities...")
	if _, err := trivy.RunInherit(ctx, stdout, stderr,
		"fs", "--format", "json", "--severity", severityFilter,
		"--scanners", "vuln", "--output", headJSON, scanPath); err != nil {
		return fmt.Errorf("trivy fs: %w", err)
	}
	headBody, err := os.ReadFile(headJSON)
	if err != nil {
		return fmt.Errorf("read head scan output %s: %w", headJSON, err)
	}
	headIDs, err := security.ExtractTrivyVulnIDs(headBody)
	if err != nil {
		return fmt.Errorf("extract head ids: %w", err)
	}

	var newIDs []string
	// modeUsed is the human-readable mode for the summary table — it
	// carries fall-back annotations ("full (worktree fallback)", "full
	// (no base ref)") that aren't part of the typed ScanMode enum.
	modeUsed := string(mode)

	switch {
	case mode == security.ScanModeDiff && in.BaseRef != "":
		fmt.Fprintf(stdout, "Diff mode: scanning base ref %q for comparison...\n", in.BaseRef)
		worktreeDir := filepath.Join(workDir, "base-worktree")

		ok := tryWorktreeAdd(ctx, gitRepo, worktreeDir, "origin/"+in.BaseRef) ||
			tryWorktreeAdd(ctx, gitRepo, worktreeDir, in.BaseRef)

		if ok {
			// Worktree was registered in .git/worktrees/<dir> by `git
			// worktree add`. Defer the de-registration so it runs on
			// every exit path — without this, a Trivy / file-read /
			// parse failure here leaks a stale worktree entry in the
			// main repo, which surfaces as a "gitdir file points to
			// non-existent location" warning on the next CLI run
			// against the same checkout.
			defer func() {
				_, _ = gitRepo.Run(ctx, "worktree", "remove", "-f", worktreeDir)
			}()
			if _, err := trivy.RunInherit(ctx, stdout, stderr,
				"fs", "--format", "json", "--severity", severityFilter,
				"--scanners", "vuln", "--output", baseJSON, worktreeDir); err != nil {
				return fmt.Errorf("trivy fs (base): %w", err)
			}

			baseBody, err := os.ReadFile(baseJSON)
			if err != nil {
				return fmt.Errorf("read base scan output %s: %w", baseJSON, err)
			}
			baseIDs, err := security.ExtractTrivyVulnIDs(baseBody)
			if err != nil {
				return fmt.Errorf("extract base ids: %w", err)
			}
			newIDs = security.DiffNewIDs(baseIDs, headIDs)
			modeUsed = "diff"
			fmt.Fprintf(stdout, "Base vulnerabilities: %d\n", len(baseIDs))
			fmt.Fprintf(stdout, "Head vulnerabilities: %d\n", len(headIDs))
			fmt.Fprintf(stdout, "New vulnerabilities:  %d\n\n", len(newIDs))
		} else {
			annot.Warningf("Could not create worktree for base ref %q. Falling back to full scan.", in.BaseRef)
			newIDs = headIDs
			modeUsed = "full (worktree fallback)"
		}
	case mode == security.ScanModeDiff && in.BaseRef == "":
		annot.Warningf("Diff mode requested but no base ref available. Running full scan.")
		newIDs = headIDs
		modeUsed = "full (no base ref)"
		fmt.Fprintf(stdout, "Total vulnerabilities: %d\n\n", len(newIDs))
	default:
		newIDs = headIDs
		modeUsed = "full"
		fmt.Fprintf(stdout, "Total vulnerabilities: %d\n\n", len(newIDs))
	}

	// Derive SARIF (via trivy convert) and the GitLab dep-scanning
	// report (via the existing transform).
	if _, err := os.Stat(headJSON); err == nil {
		fmt.Fprintln(stdout, "Converting JSON to SARIF...")
		if _, err := trivy.RunInherit(ctx, stdout, stderr,
			"convert", "--format", "sarif", "--output", sarifFile, headJSON); err != nil {
			// Match the bash `|| true` — log but don't fail.
			annot.Warningf("trivy convert failed: %v", err)
		}
		fmt.Fprintln(stdout, "Generating GitLab dependency-scanning report...")
		if _, err := TrivyToGitLabDep(TransformInput{
			InputPath:    headJSON,
			OutputPath:   glFile,
			TrivyVersion: in.TrivyVersion,
		}); err != nil {
			annot.Warningf("TrivyToGitLabDep: %v", err)
		}
	}

	rows, _ := security.FilterVulnRowsByID(headBody, newIDs)
	if err := summary.Append(ctx, security.RenderScanDepsSummary(security.ScanDepsSummaryInput{
		Mode:           modeUsed,
		FailOnSeverity: failOnSev,
		SeverityFilter: severityFilter,
		NewCount:       len(newIDs),
		NewRows:        rows,
	})); err != nil {
		return fmt.Errorf("append summary: %w", err)
	}

	if len(newIDs) > 0 {
		plural := "ies"
		if len(newIDs) == 1 {
			plural = "y"
		}
		msg := fmt.Sprintf("Found %d new vulnerabilit%s at severity %s or above", len(newIDs), plural, failOnSev)
		annot.Errorf("%s", msg)
		return errors.New(msg)
	}
	fmt.Fprintf(stdout, "✅ No new vulnerabilities found at severity %s or above\n", failOnSev)
	return nil
}

// tryWorktreeAdd attempts `git worktree add -q <dir> <ref>` and
// returns whether it succeeded. Errors are swallowed (matches the
// bash `2>/dev/null`).
func tryWorktreeAdd(ctx context.Context, gitRepo GitOps, dir, ref string) bool {
	_, err := gitRepo.Run(ctx, "worktree", "add", "-q", dir, ref)
	return err == nil
}
