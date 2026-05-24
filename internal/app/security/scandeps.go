// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// TrivyOps abstracts the trivy adapter for dependency injection.
//
//nolint:iface // see OpengrepOps — distinct consumer-defined port.
type TrivyOps interface {
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) (int, error)
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
// vulnerabilities are the set in HEAD but not in base.
//
// Returns an error when new vulnerabilities are found at or above the
// requested severity threshold.
//
// The orchestrator is intentionally thin — each phase is a named
// helper so the top-to-bottom flow reads like a sequence diagram:
// configure → scan HEAD → optionally diff against base → derive
// secondary reports → render summary → decide pass/fail.
func ScanDependencies(
	ctx context.Context,
	trivy TrivyOps,
	gitRepo GitOps,
	summary ci.SummarySink,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in ScanDependenciesInput,
) error {
	cfg := resolveScanDepsConfig(in, annot)
	printScanDepsBanner(w, cfg)

	workDir, err := os.MkdirTemp("", "scan-deps-")
	if err != nil {
		return fmt.Errorf("mkdir tmp: %w", err)
	}

	defer func() { _ = os.RemoveAll(workDir) }()

	headJSON := filepath.Join(workDir, "head-vulns.json")

	headBody, headIDs, err := scanHead(ctx, trivy, w, stderr, cfg, headJSON)
	if err != nil {
		return err
	}

	newIDs, modeUsed, err := diffAgainstBase(ctx, trivy, gitRepo, w, stderr, annot, cfg, workDir, headIDs)
	if err != nil {
		return err
	}

	deriveSecondaryReports(ctx, trivy, w, annot, cfg, headJSON)

	if err := writeScanDepsSummary(ctx, summary, cfg, modeUsed, headBody, newIDs); err != nil {
		return err
	}

	return reportScanDepsVerdict(w, annot, cfg, newIDs)
}

// scanDepsConfig is the resolved, defaulted input view used internally
// by every phase. Lifts the cmp.Or defaulting out of the orchestrator.
type scanDepsConfig struct {
	failOnSev      string
	mode           security.ScanMode
	scanPath       string
	sarifFile      string
	gitlabDepFile  string
	trivyVersion   string
	baseRef        string
	severityFilter string
}

func resolveScanDepsConfig(in ScanDependenciesInput, annot output.Annotator) scanDepsConfig {
	cfg := scanDepsConfig{
		failOnSev:     cmp.Or(in.FailOnSeverity, string(security.DepSeverityCritical)),
		mode:          cmp.Or(in.ScanMode, security.ScanModeDiff),
		scanPath:      cmp.Or(in.ScanPath, "."),
		sarifFile:     cmp.Or(in.SARIFFile, security.DefaultTrivySARIFFile),
		gitlabDepFile: cmp.Or(in.GitLabDepFile, security.DefaultTrivyGitLabDepFile),
		trivyVersion:  in.TrivyVersion,
		baseRef:       in.BaseRef,
	}
	cfg.severityFilter = security.MapTrivyFailSeverity(cfg.failOnSev)

	if in.FailOnSeverity != "" && !security.IsKnownTrivyFailSeverity(in.FailOnSeverity) {
		annot.Warningf("Unknown severity %q, defaulting to critical", in.FailOnSeverity)
	}

	return cfg
}

func printScanDepsBanner(w io.Writer, cfg scanDepsConfig) {
	_, _ = fmt.Fprintf(w, "🔍 Dependency vulnerability scan\n")
	_, _ = fmt.Fprintf(w, "   Severity threshold: %s\n", cfg.failOnSev)
	_, _ = fmt.Fprintf(w, "   Scan mode: %s\n", cfg.mode)
	_, _ = fmt.Fprintf(w, "   Scan path: %s\n\n", cfg.scanPath)
}

// scanHead runs trivy against the working tree and returns the raw JSON
// body alongside the extracted vulnerability IDs.
func scanHead(
	ctx context.Context,
	trivy TrivyOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	cfg scanDepsConfig,
	outputJSON string,
) ([]byte, []string, error) {
	_, _ = fmt.Fprintln(w, "Scanning HEAD for vulnerabilities...")

	if _, runErr := trivy.RunInherit(ctx, w, stderr,
		"fs", "--format", "json", "--severity", cfg.severityFilter,
		"--scanners", "vuln", "--output", outputJSON, cfg.scanPath); runErr != nil {
		return nil, nil, fmt.Errorf("trivy fs: %w", runErr)
	}

	body, err := os.ReadFile(outputJSON) //nolint:gosec // outputJSON is locally-built filename.
	if err != nil {
		return nil, nil, fmt.Errorf("read head scan output %s: %w", outputJSON, err)
	}

	ids, err := security.ExtractTrivyVulnIDs(body)
	if err != nil {
		return nil, nil, fmt.Errorf("extract head ids: %w", err)
	}

	return body, ids, nil
}

// diffAgainstBase resolves which vulnerability IDs are "new" given the
// configured mode. The returned modeUsed is the human-readable mode
// string for the summary table — it carries fall-back annotations
// ("full (worktree fallback)", "full (no base ref)") that aren't part
// of the typed ScanMode enum.
func diffAgainstBase(
	ctx context.Context,
	trivy TrivyOps,
	gitRepo GitOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	cfg scanDepsConfig,
	workDir string,
	headIDs []string,
) ([]string, string, error) {
	switch {
	case cfg.mode == security.ScanModeDiff && cfg.baseRef != "":
		return scanBaseRefViaWorktree(ctx, trivy, gitRepo, w, stderr, annot, cfg, workDir, headIDs)
	case cfg.mode == security.ScanModeDiff && cfg.baseRef == "":
		annot.Warningf("Diff mode requested but no base ref available. Running full scan.")

		_, _ = fmt.Fprintf(w, "Total vulnerabilities: %d\n\n", len(headIDs))

		return headIDs, "full (no base ref)", nil
	default:
		_, _ = fmt.Fprintf(w, "Total vulnerabilities: %d\n\n", len(headIDs))

		return headIDs, "full", nil
	}
}

// scanBaseRefViaWorktree creates a git worktree pointing at the base
// ref, runs trivy against it, and returns the new-IDs diff. Worktree
// creation failure (network / shallow checkout) falls back to a full
// scan; once the worktree exists, base-scan failures are fatal — the
// operator should not see a "clean" verdict that was actually a
// silently-skipped comparison.
//
// The worktree de-registration is deferred via diffErr capture so a
// Trivy / file-read / parse failure here doesn't leak a stale entry
// in .git/worktrees/ — that would surface as a "gitdir file points
// to non-existent location" warning on the next CLI run against the
// same checkout.
func scanBaseRefViaWorktree(
	ctx context.Context,
	trivy TrivyOps,
	gitRepo GitOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	cfg scanDepsConfig,
	workDir string,
	headIDs []string,
) ([]string, string, error) {
	_, _ = fmt.Fprintf(w, "Diff mode: scanning base ref %q for comparison...\n", cfg.baseRef)

	worktreeDir := filepath.Join(workDir, "base-worktree")

	ok := tryWorktreeAdd(ctx, gitRepo, worktreeDir, "origin/"+cfg.baseRef) ||
		tryWorktreeAdd(ctx, gitRepo, worktreeDir, cfg.baseRef)
	if !ok {
		annot.Warningf("Could not create worktree for base ref %q. Falling back to full scan.", cfg.baseRef)

		return headIDs, "full (worktree fallback)", nil
	}

	defer func() {
		_, _ = gitRepo.Run(ctx, "worktree", "remove", "-f", worktreeDir)
	}()

	baseJSON := filepath.Join(workDir, "base-vulns.json")
	if _, runErr := trivy.RunInherit(ctx, w, stderr,
		"fs", "--format", "json", "--severity", cfg.severityFilter,
		"--scanners", "vuln", "--output", baseJSON, worktreeDir); runErr != nil {
		return nil, "", fmt.Errorf("trivy fs (base): %w", runErr)
	}

	baseBody, err := os.ReadFile(baseJSON) //nolint:gosec // baseJSON is locally-built filename.
	if err != nil {
		return nil, "", fmt.Errorf("read base scan output %s: %w", baseJSON, err)
	}

	baseIDs, err := security.ExtractTrivyVulnIDs(baseBody)
	if err != nil {
		return nil, "", fmt.Errorf("extract base ids: %w", err)
	}

	newIDs := security.DiffNewIDs(baseIDs, headIDs)
	_, _ = fmt.Fprintf(w, "Base vulnerabilities: %d\n", len(baseIDs))
	_, _ = fmt.Fprintf(w, "Head vulnerabilities: %d\n", len(headIDs))
	_, _ = fmt.Fprintf(w, "New vulnerabilities:  %d\n\n", len(newIDs))

	return newIDs, "diff", nil
}

// deriveSecondaryReports converts the head-scan JSON into SARIF (for
// GitHub Code Scanning) and the GitLab dependency-scanning JSON
// report. Both are best-effort: failures log a warning but don't
// abort the scan.
func deriveSecondaryReports(
	ctx context.Context,
	trivy TrivyOps,
	w io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	cfg scanDepsConfig,
	headJSON string,
) {
	if _, err := os.Stat(headJSON); err != nil {
		return
	}

	_, _ = fmt.Fprintln(w, "Converting JSON to SARIF...")

	if _, err := trivy.RunInherit(ctx, w, w,
		"convert", "--format", "sarif", "--output", cfg.sarifFile, headJSON); err != nil {
		annot.Warningf("trivy convert failed: %v", err)
	}

	_, _ = fmt.Fprintln(w, "Generating GitLab dependency-scanning report...")

	if _, err := TrivyToGitLabDep(TransformInput{
		InputPath:    headJSON,
		OutputPath:   cfg.gitlabDepFile,
		TrivyVersion: cfg.trivyVersion,
	}); err != nil {
		annot.Warningf("TrivyToGitLabDep: %v", err)
	}
}

func writeScanDepsSummary(
	ctx context.Context,
	summary ci.SummarySink,
	cfg scanDepsConfig,
	modeUsed string,
	headBody []byte,
	newIDs []string,
) error {
	rows, _ := security.FilterVulnRowsByID(headBody, newIDs)
	if err := summary.Append(ctx, security.RenderScanDepsSummary(security.ScanDepsSummaryInput{
		Mode:           modeUsed,
		FailOnSeverity: cfg.failOnSev,
		SeverityFilter: cfg.severityFilter,
		NewCount:       len(newIDs),
		NewRows:        rows,
	})); err != nil {
		return fmt.Errorf("append summary: %w", err)
	}

	return nil
}

func reportScanDepsVerdict(w io.Writer, annot output.Annotator, cfg scanDepsConfig, newIDs []string) error {
	if len(newIDs) == 0 {
		_, _ = fmt.Fprintf(w, "✅ No new vulnerabilities found at severity %s or above\n", cfg.failOnSev)

		return nil
	}

	noun := "vulnerabilities"
	if len(newIDs) == 1 {
		noun = "vulnerability"
	}

	msg := fmt.Sprintf("Found %d new %s at severity %s or above", len(newIDs), noun, cfg.failOnSev)
	annot.Errorf("%s", msg)
	// Domain rule failure (scan threshold exceeded) — wrap so the CLI
	// exits with ExitCodeValidation (1), not ExitCodeSoftware (70).
	return fmt.Errorf("%s: %w", msg, errs.ErrValidation)
}

// tryWorktreeAdd attempts `git worktree add -q <dir> <ref>` and
// returns whether it succeeded. Errors are swallowed.
func tryWorktreeAdd(ctx context.Context, gitRepo GitOps, dir, ref string) bool {
	_, err := gitRepo.Run(ctx, "worktree", "add", "-q", dir, ref)

	return err == nil
}
