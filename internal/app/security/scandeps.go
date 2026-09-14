// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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
//
//nolint:cyclop // validate scope and destinations before the ordered HEAD/base/report phases.
func ScanDependencies(
	ctx context.Context,
	trivy TrivyOps,
	gitRepo GitOps,
	summary ci.SummarySink,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in ScanDependenciesInput,
) error {
	cfg, err := resolveScanDepsConfig(in)
	if err != nil {
		return err
	}

	if err = validateReportPaths(cfg.sarifFile, cfg.gitlabDepFile); err != nil {
		return err
	}

	if cfg.mode == security.ScanModeDiff && cfg.baseRef != "" { //nolint:nestif // one preflight scope binds both scans without moving process cwd.
		root, err := gitRepo.Run(ctx, "rev-parse", "--show-toplevel") //nolint:govet // scope-local errors are handled before any scan effect.
		if err != nil {
			return fmt.Errorf("resolve comparison repository: %w", err)
		}

		root = strings.TrimSpace(root)
		if root == "" {
			return fmt.Errorf("comparison repository root is empty: %w", errs.ErrUsage)
		}

		absolute, err := filepath.Abs(cfg.scanPath)
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, absolute)
		if err != nil || !pathsafe.Relative(relative) {
			return fmt.Errorf("dependency diff scope must stay inside the repository: %w", errs.ErrUsage)
		}

		info, err := os.Lstat(absolute)
		if err != nil {
			return fmt.Errorf("inspect dependency scope: %w", err)
		}

		parent := filepath.Dir(absolute)
		if info.IsDir() {
			parent = absolute
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("dependency scope must be a real directory or regular file: %w", errs.ErrUsage)
		}

		scopeRoot, err := pathsafe.OpenRoot(parent)
		if err != nil {
			return err
		}

		_ = scopeRoot.Close()
		cfg.relativeScope = relative
		cfg.scanPath = absolute
	}

	if aliasErr := validateScanInputReports(cfg.scanPath, cfg.sarifFile, cfg.gitlabDepFile); aliasErr != nil {
		return aliasErr
	}

	workDir, err := prepareScanReports(cfg.sarifFile, cfg.gitlabDepFile)
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(workDir) }()

	printScanDepsBanner(w, cfg)

	deps := scanDepsDeps{trivy: trivy, gitRepo: gitRepo, w: w, stderr: stderr, annot: annot}

	headJSON := filepath.Join(workDir, "head-vulns.json")

	headBody, headFindings, err := scanHead(ctx, deps, cfg, headJSON)
	if err != nil {
		return err
	}

	newFindings, modeUsed, err := diffAgainstBase(ctx, deps, cfg, workDir, headFindings)
	if err != nil {
		return err
	}

	deriveSecondaryReports(ctx, deps, cfg, headJSON)

	if err := writeScanDepsSummary(ctx, summary, cfg, modeUsed, headBody, newFindings); err != nil {
		return err
	}

	return reportScanDepsVerdict(w, annot, cfg, newFindings)
}

// scanDepsDeps is the collaborator set the dependency-scan phases share:
// the trivy scanner, the git repo (for the base-ref worktree), and the
// three sinks they narrate on (w, stderr, annot). All thread unchanged from
// ScanDependencies through the phase functions, which gave the two worktree
// phases nine-parameter signatures.
//
// As with the layerDeps precedent in internal/app/sbom, the set narrows at
// the edges: scanHead does not touch git, deriveSecondaryReports does not
// touch stderr, so those leave a field or two unused. The two leaves that
// narrow to w-only (printScanDepsBanner, reportScanDepsVerdict) keep explicit
// parameters — passing the whole set to print a banner would hide what it
// uses.
type scanDepsDeps struct {
	trivy   TrivyOps
	gitRepo GitOps
	w       io.Writer // progress, user-facing
	stderr  io.Writer // underlying tool output
	annot   output.Annotator
}

// scanDepsConfig is the resolved, defaulted input view used internally
// by every phase. Lifts the cmp.Or defaulting out of the orchestrator.
type scanDepsConfig struct {
	relativeScope  string
	failOnSev      string
	mode           security.ScanMode
	scanPath       string
	sarifFile      string
	gitlabDepFile  string
	trivyVersion   string
	baseRef        string
	severityFilter string
}

// resolveScanDepsConfig resolves and defaults the inputs, refusing a
// threshold it does not recognise.
//
// The refusal replaces a warn-and-continue that fell back to CRITICAL --
// the NARROWEST filter. Because the resolved filter is passed to trivy as
// --severity, that fallback did not merely stop HIGH and MEDIUM findings
// from failing the build: trivy never reported them, so they were absent
// from the SARIF and the GitLab report too. A mistyped gate silently
// became the strictest-looking and least protective one.
//
// Two spellings made this easy to hit: "medium" (trivy's own name for
// that band, now accepted as "moderate") and "CRITICAL,HIGH" (the
// grammar the sibling `security scan container` documents for the
// identically-named flag). The sibling `NormalizeOpengrepFailSeverity`
// has always refused unknown input; this brings the two into line.
func resolveScanDepsConfig(in ScanDependenciesInput) (scanDepsConfig, error) {
	if in.ScanMode != "" && in.ScanMode != security.ScanModeDiff && in.ScanMode != security.ScanModeFull {
		return scanDepsConfig{}, fmt.Errorf("unsupported dependency scan mode %q: %w", in.ScanMode, errs.ErrUsage)
	}

	if in.FailOnSeverity != "" && !security.ParseDepSeverity(in.FailOnSeverity).IsKnown() {
		return scanDepsConfig{}, fmt.Errorf(
			"scan dependencies: unknown --fail-on-severity %q (want low, moderate, high or critical; note this flag takes ONE word, unlike `security scan container` which takes a comma-list): %w",
			in.FailOnSeverity, errs.ErrUsage)
	}

	cfg := scanDepsConfig{
		failOnSev:     cmp.Or(in.FailOnSeverity, string(security.DepSeverityCritical)),
		mode:          cmp.Or(in.ScanMode, security.ScanModeDiff),
		scanPath:      cmp.Or(in.ScanPath, "."),
		sarifFile:     cmp.Or(in.SARIFFile, security.DefaultTrivySARIFFile),
		gitlabDepFile: cmp.Or(in.GitLabDepFile, security.DefaultTrivyGitLabDepFile),
		trivyVersion:  in.TrivyVersion,
		baseRef:       in.BaseRef,
	}
	cfg.severityFilter = security.ParseDepSeverity(cfg.failOnSev).TrivyFilter()

	return cfg, nil
}

func printScanDepsBanner(w io.Writer, cfg scanDepsConfig) {
	_, _ = fmt.Fprintf(w, "🔍 Dependency vulnerability scan\n")
	_, _ = fmt.Fprintf(w, "   Severity threshold: %s\n", cfg.failOnSev)
	_, _ = fmt.Fprintf(w, "   Scan mode: %s\n", cfg.mode)
	_, _ = fmt.Fprintf(w, "   Scan path: %s\n\n", cfg.scanPath)
}

// scanHead runs trivy against the working tree and returns the raw JSON
// body alongside the extracted findings.
func scanHead(
	ctx context.Context,
	deps scanDepsDeps,
	cfg scanDepsConfig,
	outputJSON string,
) ([]byte, []security.VulnFinding, error) {
	_, _ = fmt.Fprintln(deps.w, "Scanning HEAD for vulnerabilities...")

	code, runErr := deps.trivy.RunInherit(ctx, deps.w, deps.stderr,
		"fs", "--format", "json", "--severity", cfg.severityFilter,
		"--scanners", "vuln", "--output", outputJSON, cfg.scanPath)
	if runErr != nil {
		return nil, nil, fmt.Errorf("trivy fs: %w", runErr)
	}

	if code != 0 {
		return nil, nil, fmt.Errorf("trivy fs exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	body, err := readJSONScanReport(outputJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("read head scan output %s: %w", outputJSON, err)
	}

	findings, err := security.ExtractTrivyFindings(body)
	if err != nil {
		return nil, nil, fmt.Errorf("extract head findings: %w", err)
	}

	return body, findings, nil
}

// diffAgainstBase resolves which findings are "new" given the
// configured mode. The returned modeUsed is the human-readable mode
// string for the summary table — it carries fall-back annotations
// ("full (worktree fallback)", "full (no base ref)") that aren't part
// of the typed ScanMode enum.
func diffAgainstBase(
	ctx context.Context,
	deps scanDepsDeps,
	cfg scanDepsConfig,
	workDir string,
	headFindings []security.VulnFinding,
) ([]security.VulnFinding, string, error) {
	switch {
	case cfg.mode == security.ScanModeDiff && cfg.baseRef != "":
		return scanBaseRefViaWorktree(ctx, deps, cfg, workDir, headFindings)
	case cfg.mode == security.ScanModeDiff && cfg.baseRef == "":
		deps.annot.Warningf("Diff mode requested but no base ref available. Running full scan.")

		_, _ = fmt.Fprintf(deps.w, "Total vulnerabilities: %d\n\n", len(headFindings))

		return headFindings, "full (no base ref)", nil
	default:
		_, _ = fmt.Fprintf(deps.w, "Total vulnerabilities: %d\n\n", len(headFindings))

		return headFindings, "full", nil
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
//
//nolint:nonamedreturns // deferred bounded cleanup joins its failure with the primary operation error.
func scanBaseRefViaWorktree(
	ctx context.Context,
	deps scanDepsDeps,
	cfg scanDepsConfig,
	workDir string,
	headFindings []security.VulnFinding,
) (findings []security.VulnFinding, mode string, err error) {
	_, _ = fmt.Fprintf(deps.w, "Diff mode: scanning base ref %q for comparison...\n", cfg.baseRef)

	worktreeDir := filepath.Join(workDir, "base-worktree")

	ok := tryWorktreeAdd(ctx, deps.gitRepo, worktreeDir, "origin/"+cfg.baseRef) ||
		tryWorktreeAdd(ctx, deps.gitRepo, worktreeDir, cfg.baseRef)
	if !ok {
		deps.annot.Warningf("Could not create worktree for base ref %q. Falling back to full scan.", cfg.baseRef)

		return headFindings, "full (worktree fallback)", nil
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if _, cleanupErr := deps.gitRepo.Run(cleanupCtx, "worktree", "remove", "-f", worktreeDir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove dependency worktree: %w", cleanupErr))
		}
	}()

	baseJSON := filepath.Join(workDir, "base-vulns.json")

	code, runErr := deps.trivy.RunInherit(ctx, deps.w, deps.stderr,
		"fs", "--format", "json", "--severity", cfg.severityFilter,
		"--scanners", "vuln", "--output", baseJSON, filepath.Join(worktreeDir, cfg.relativeScope))
	if runErr != nil {
		return nil, "", fmt.Errorf("trivy fs (base): %w", runErr)
	}

	if code != 0 {
		return nil, "", fmt.Errorf("trivy fs (base) exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	baseBody, err := readJSONScanReport(baseJSON)
	if err != nil {
		return nil, "", fmt.Errorf("read base scan output %s: %w", baseJSON, err)
	}

	baseFindings, err := security.ExtractTrivyFindings(baseBody)
	if err != nil {
		return nil, "", fmt.Errorf("extract base findings: %w", err)
	}

	newFindings := security.DiffNewFindings(baseFindings, headFindings)
	_, _ = fmt.Fprintf(deps.w, "Base vulnerabilities: %d\n", len(baseFindings))
	_, _ = fmt.Fprintf(deps.w, "Head vulnerabilities: %d\n", len(headFindings))
	_, _ = fmt.Fprintf(deps.w, "New vulnerabilities:  %d\n\n", len(newFindings))

	return newFindings, "diff", nil
}

// deriveSecondaryReports converts the head-scan JSON into SARIF (for
// GitHub Code Scanning) and the GitLab dependency-scanning JSON
// report. Both are best-effort: failures log a warning but don't
// abort the scan.
func deriveSecondaryReports(
	ctx context.Context,
	deps scanDepsDeps,
	cfg scanDepsConfig,
	headJSON string,
) {
	if _, err := os.Stat(headJSON); err != nil {
		return
	}

	_, _ = fmt.Fprintln(deps.w, "Converting JSON to SARIF...")
	sarifPath := filepath.Join(filepath.Dir(headJSON), "report.sarif")

	code, err := deps.trivy.RunInherit(ctx, deps.w, deps.stderr,
		"convert", "--format", "sarif", "--output", sarifPath, headJSON)
	if err != nil {
		deps.annot.Warningf("trivy convert failed: %v", err)
	} else if code != 0 {
		deps.annot.Warningf("trivy convert exited with status %d", code)
	} else if err := publishScanReport(sarifPath, cfg.sarifFile, true); err != nil {
		deps.annot.Warningf("publish converted SARIF: %v", err)
	}

	_, _ = fmt.Fprintln(deps.w, "Generating GitLab dependency-scanning report...")

	gitlabPath := filepath.Join(filepath.Dir(headJSON), "gitlab.json")
	if _, err := TrivyToGitLabDep(TransformInput{
		InputPath:    headJSON,
		OutputPath:   gitlabPath,
		TrivyVersion: cfg.trivyVersion,
	}); err != nil {
		deps.annot.Warningf("TrivyToGitLabDep: %v", err)
	} else if err := publishScanReport(gitlabPath, cfg.gitlabDepFile, true); err != nil {
		deps.annot.Warningf("publish GitLab dependency report: %v", err)
	}
}

func writeScanDepsSummary(
	ctx context.Context,
	summary ci.SummarySink,
	cfg scanDepsConfig,
	modeUsed string,
	headBody []byte,
	newFindings []security.VulnFinding,
) error {
	rows, _ := security.FilterVulnRows(headBody, newFindings)

	// Package names and versions come from the scanned lockfiles, which a
	// pull request controls. These cells are plain text, not code spans, so
	// a pipe, a newline, a link or an HTML tag in one must render as the
	// characters rather than reshape the table or the page.
	for index, row := range rows {
		rows[index] = security.VulnRow{
			ID:        domainsummary.LiteralText(row.ID),
			Severity:  domainsummary.LiteralText(row.Severity),
			Package:   domainsummary.LiteralText(row.Package),
			Installed: domainsummary.LiteralText(row.Installed),
			Fixed:     domainsummary.LiteralText(row.Fixed),
		}
	}

	if err := summary.Append(ctx, security.RenderScanDepsSummary(security.ScanDepsSummaryInput{
		Mode:           modeUsed,
		FailOnSeverity: cfg.failOnSev,
		SeverityFilter: cfg.severityFilter,
		NewCount:       len(newFindings),
		NewRows:        rows,
	})); err != nil {
		return fmt.Errorf("append summary: %w", err)
	}

	return nil
}

func reportScanDepsVerdict(w io.Writer, annot output.Annotator, cfg scanDepsConfig, newFindings []security.VulnFinding) error {
	if len(newFindings) == 0 {
		_, _ = fmt.Fprintf(w, "%s No new vulnerabilities found at severity %s or above\n", clicolor.Check(w), cfg.failOnSev)

		return nil
	}

	noun := "vulnerabilities"
	if len(newFindings) == 1 {
		noun = "vulnerability"
	}

	msg := fmt.Sprintf("Found %d new %s at severity %s or above", len(newFindings), noun, cfg.failOnSev)
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
