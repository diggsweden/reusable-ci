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
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// OpengrepOps abstracts the opengrep adapter for dependency injection.
type OpengrepOps interface {
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error)
}

// RunOpengrepInput drives RunOpengrep. The defaults mirror the bash's
// DEFAULT_* constants.
type RunOpengrepInput struct {
	Config             string            // empty → "p/default"
	FailOnSeverity     string            // empty → "high"
	TargetPath         string            // empty → "."
	JSONFile           string            // empty → "opengrep-results.json"
	SARIFFile          string            // empty → "opengrep-results.sarif"
	TextFile           string            // empty → "opengrep-results.txt"
	GitLabSASTFile     string            // empty → "opengrep-results.gitlab-sast.json"
	Platform           provider.Platform // passed to summary platform context
	HasCodeScanningTok bool
	RunURL             string
}

// RunOpengrep installs the binary expectation (no-op here — the
// runtime image bakes it in), invokes opengrep with the canonical
// arg set, parses the JSON output for counts, writes the step
// summary, and emits the six OutputSink values.
//
// Mirrors scripts/security/run-opengrep.sh end-to-end. Exit code
// semantics:
//   - opengrep itself exited non-zero → write the failure summary and
//     return an error containing the exit code (mirrors `exit "$scan_exit"`).
//   - the scan succeeded but findings meet the threshold → return an
//     error after emitting outputs + summary.
//   - clean → nil error.
func RunOpengrep(
	ctx context.Context,
	ops OpengrepOps,
	out ci.OutputSink,
	summary ci.SummarySink,
	stdout, stderr io.Writer,
	annot output.Annotator,
	in RunOpengrepInput,
) error {
	config := cmp.Or(in.Config, "p/default")
	failOnSevRaw := cmp.Or(in.FailOnSeverity, "high")
	targetPath := cmp.Or(in.TargetPath, ".")
	jsonFile := cmp.Or(in.JSONFile, "opengrep-results.json")
	sarifFile := cmp.Or(in.SARIFFile, "opengrep-results.sarif")
	textFile := cmp.Or(in.TextFile, "opengrep-results.txt")
	gitlabFile := cmp.Or(in.GitLabSASTFile, "opengrep-results.gitlab-sast.json")

	failOnSev, err := security.NormalizeOpengrepFailSeverity(failOnSevRaw)
	if err != nil {
		annot.Errorf("%v", err)
		return err
	}

	configList, err := security.ParseConfigList(config)
	if err != nil {
		annot.Errorf("%v", err)
		return err
	}

	platform := security.OpengrepPlatformContext{
		Platform:             in.Platform,
		HasCodeScanningToken: in.HasCodeScanningTok,
		RunURL:               in.RunURL,
	}

	args := []string{
		"scan",
		"--quiet",
		"--disable-version-check",
		"--exclude", ".github-shared",
		"--taint-intrafile",
		"--dataflow-traces",
		"--json-output", jsonFile,
		"--sarif-output", sarifFile,
		"--text-output", textFile,
		"--gitlab-sast-output", gitlabFile,
	}
	for _, c := range configList {
		args = append(args, "--config", c)
	}
	args = append(args, targetPath)

	fmt.Fprintf(stdout, "Running OpenGrep with config '%s' on '%s'...\n", config, targetPath)
	exitCode, runErr := ops.RunInherit(ctx, stdout, stderr, args...)
	if runErr != nil {
		return fmt.Errorf("opengrep: %w", runErr)
	}
	if exitCode != 0 {
		md := security.RenderOpengrepFailureSummary(security.OpengrepFailureSummaryInput{
			Config:     config,
			TargetPath: targetPath,
			ExitCode:   exitCode,
			Platform:   platform,
		})
		if err := summary.Append(ctx, md); err != nil {
			return fmt.Errorf("append summary: %w", err)
		}
		return fmt.Errorf("opengrep exited with status %d", exitCode)
	}

	// Read the JSON file to count findings — the bash uses grep -o.
	jsonBody, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("read opengrep json output %s: %w", jsonFile, err)
	}
	counts := security.CountOpengrepFindings(string(jsonBody))
	thresholdFailure := security.OpengrepHasFindingsMeetingThreshold(
		failOnSev, counts.FindingsTotal, counts.ErrorTotal, counts.WarningTotal,
	)

	// Cosmetic-only — the text-output file feeds a <details> excerpt
	// in the step summary. A missing / unreadable file falls back to
	// an empty excerpt rather than failing the scan.
	textBody, _ := os.ReadFile(textFile)
	excerpt := security.HeadN(string(textBody), 120)

	md, scanResult := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:           config,
		TargetPath:       targetPath,
		FailOnSeverity:   failOnSev,
		Counts:           counts,
		ThresholdFailure: thresholdFailure,
		TextExcerpt:      excerpt,
		Platform:         platform,
	})
	if err := summary.Append(ctx, md); err != nil {
		return fmt.Errorf("append summary: %w", err)
	}

	type kv struct{ k, v string }
	for _, p := range []kv{
		{"opengrep-result", scanResult},
		{"opengrep-findings-total", strconv.Itoa(counts.FindingsTotal)},
		{"opengrep-findings-error", strconv.Itoa(counts.ErrorTotal)},
		{"opengrep-findings-warning", strconv.Itoa(counts.WarningTotal)},
		{"opengrep-findings-info", strconv.Itoa(counts.InfoTotal)},
		{"opengrep-fail-threshold", string(failOnSev)},
	} {
		if err := out.Set(ctx, p.k, p.v); err != nil {
			return fmt.Errorf("set %s: %w", p.k, err)
		}
	}

	if thresholdFailure {
		msg := fmt.Sprintf("OpenGrep found findings meeting fail threshold '%s'", failOnSev)
		annot.Errorf("%s", msg)
		return errors.New(msg)
	}
	fmt.Fprintf(stdout, "OpenGrep completed successfully with %d findings\n", counts.FindingsTotal)
	return nil
}
