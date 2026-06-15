// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// OpengrepOps abstracts the opengrep adapter for dependency injection.
//
// distinct adapter; merging would couple two unrelated scanners.
//
//nolint:iface // consumer-defined port — same shape as TrivyOps but a
type OpengrepOps interface {
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) (int, error)
}

// RunOpengrepInput drives RunOpengrep.
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

// RunOpengrep invokes opengrep with the canonical arg set, parses the
// JSON output for counts, writes the step summary, and emits the six
// OutputSink values.
//
// Exit-code semantics:
//   - opengrep itself exited non-zero → write the failure summary and
//     return an error containing the exit code.
//   - scan succeeded but findings meet the threshold → return an error
//     after emitting outputs + summary.
//   - clean → nil error.
//
// The orchestrator is intentionally thin — each phase is a named
// helper so the top-to-bottom flow reads as: configure → invoke →
// parse → summary → emit outputs → verdict.
func RunOpengrep(
	ctx context.Context,
	ops OpengrepOps,
	out ci.OutputSink,
	summary ci.SummarySink,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in RunOpengrepInput,
) error {
	cfg, err := resolveOpengrepConfig(in, annot)
	if err != nil {
		return err
	}

	exitCode, err := invokeOpengrep(ctx, ops, w, stderr, cfg)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return reportOpengrepCrash(ctx, summary, cfg, exitCode)
	}

	counts, excerpt, err := readOpengrepResults(cfg)
	if err != nil {
		return err
	}

	thresholdFailure := security.OpengrepHasFindingsMeetingThreshold(
		cfg.failOnSev, counts.FindingsTotal, counts.ErrorTotal, counts.WarningTotal,
	)

	scanResult, err := writeOpengrepSummary(ctx, summary, cfg, counts, excerpt, thresholdFailure)
	if err != nil {
		return err
	}

	if err := emitOpengrepOutputs(ctx, out, cfg, counts, scanResult); err != nil {
		return err
	}

	return reportOpengrepVerdict(w, annot, cfg, counts, thresholdFailure)
}

// opengrepConfig is the resolved, defaulted view of RunOpengrepInput
// used by every phase.
type opengrepConfig struct {
	config     string
	configList []string
	failOnSev  security.OpengrepSeverity
	targetPath string
	jsonFile   string
	sarifFile  string
	textFile   string
	gitlabFile string
	platform   security.OpengrepPlatformContext
}

func resolveOpengrepConfig(in RunOpengrepInput, annot output.Annotator) (opengrepConfig, error) {
	cfg := opengrepConfig{
		config:     cmp.Or(in.Config, "p/default"),
		targetPath: cmp.Or(in.TargetPath, "."),
		jsonFile:   cmp.Or(in.JSONFile, "opengrep-results.json"),
		sarifFile:  cmp.Or(in.SARIFFile, "opengrep-results.sarif"),
		textFile:   cmp.Or(in.TextFile, "opengrep-results.txt"),
		gitlabFile: cmp.Or(in.GitLabSASTFile, "opengrep-results.gitlab-sast.json"),
		platform: security.OpengrepPlatformContext{
			Platform:             in.Platform,
			HasCodeScanningToken: in.HasCodeScanningTok,
			RunURL:               in.RunURL,
		},
	}

	sev, err := security.NormalizeOpengrepFailSeverity(cmp.Or(in.FailOnSeverity, "high"))
	if err != nil {
		annot.Errorf("%v", err)

		return opengrepConfig{}, err
	}

	cfg.failOnSev = sev

	list, err := security.ParseConfigList(cfg.config)
	if err != nil {
		annot.Errorf("%v", err)

		return opengrepConfig{}, err
	}

	cfg.configList = list

	return cfg, nil
}

// invokeOpengrep runs the scan binary and returns its exit code. A
// non-zero exit code is *not* an error — the caller distinguishes
// "scan crashed" (return err) from "scan ran, has findings" (handle
// via exit code).
func invokeOpengrep(
	ctx context.Context,
	ops OpengrepOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	cfg opengrepConfig,
) (int, error) {
	args := make([]string, 0, 15+2*len(cfg.configList)+1)
	args = append(args,
		"scan",
		"--quiet",
		"--disable-version-check",
		"--exclude", ".github-shared",
		"--taint-intrafile",
		"--dataflow-traces",
		"--json-output", cfg.jsonFile,
		"--sarif-output", cfg.sarifFile,
		"--text-output", cfg.textFile,
		"--gitlab-sast-output", cfg.gitlabFile,
	)

	for _, c := range cfg.configList {
		args = append(args, "--config", c)
	}

	args = append(args, cfg.targetPath)

	_, _ = fmt.Fprintf(w, "Running OpenGrep with config '%s' on '%s'...\n", cfg.config, cfg.targetPath)

	exitCode, err := ops.RunInherit(ctx, w, stderr, args...)
	if err != nil {
		return 0, fmt.Errorf("opengrep: %w", err)
	}

	return exitCode, nil
}

func reportOpengrepCrash(ctx context.Context, summary ci.SummarySink, cfg opengrepConfig, exitCode int) error {
	md := security.RenderOpengrepFailureSummary(security.OpengrepFailureSummaryInput{
		Config:     cfg.config,
		TargetPath: cfg.targetPath,
		ExitCode:   exitCode,
		Platform:   cfg.platform,
	})
	if err := summary.Append(ctx, md); err != nil {
		return fmt.Errorf("append summary: %w", err)
	}

	return fmt.Errorf("opengrep exited with status %d: %w", exitCode, errs.ErrDependencyUnavailable)
}

// readOpengrepResults parses the JSON output for counts and reads the
// text-output file for the summary excerpt. The text-output read is
// cosmetic-only — a missing / unreadable file falls back to an empty
// excerpt rather than failing the scan.
func readOpengrepResults(cfg opengrepConfig) (security.OpengrepCounts, string, error) {
	jsonBody, err := os.ReadFile(cfg.jsonFile)
	if err != nil {
		return security.OpengrepCounts{}, "", fmt.Errorf("read opengrep json output %s: %w", cfg.jsonFile, err)
	}

	counts := security.CountOpengrepFindings(string(jsonBody))
	textBody, _ := os.ReadFile(cfg.textFile)
	excerpt := security.HeadN(string(textBody), 120)

	return counts, excerpt, nil
}

func writeOpengrepSummary(
	ctx context.Context,
	summary ci.SummarySink,
	cfg opengrepConfig,
	counts security.OpengrepCounts,
	excerpt string,
	thresholdFailure bool,
) (string, error) {
	md, scanResult := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:           cfg.config,
		TargetPath:       cfg.targetPath,
		FailOnSeverity:   cfg.failOnSev,
		Counts:           counts,
		ThresholdFailure: thresholdFailure,
		TextExcerpt:      excerpt,
		Platform:         cfg.platform,
	})
	if err := summary.Append(ctx, md); err != nil {
		return "", fmt.Errorf("append summary: %w", err)
	}

	return scanResult, nil
}

func emitOpengrepOutputs(
	ctx context.Context,
	out ci.OutputSink,
	cfg opengrepConfig,
	counts security.OpengrepCounts,
	scanResult string,
) error {
	type kv struct{ k, v string }
	for _, p := range []kv{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		{"opengrep-result", scanResult},
		{"opengrep-findings-total", strconv.Itoa(counts.FindingsTotal)},
		{"opengrep-findings-error", strconv.Itoa(counts.ErrorTotal)},
		{"opengrep-findings-warning", strconv.Itoa(counts.WarningTotal)},
		{"opengrep-findings-info", strconv.Itoa(counts.InfoTotal)},
		{"opengrep-fail-threshold", string(cfg.failOnSev)},
	} {
		if err := out.Set(ctx, p.k, p.v); err != nil {
			return fmt.Errorf("set %s: %w", p.k, err)
		}
	}

	return nil
}

func reportOpengrepVerdict(
	w io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	cfg opengrepConfig,
	counts security.OpengrepCounts,
	thresholdFailure bool,
) error {
	if thresholdFailure {
		msg := fmt.Sprintf("OpenGrep found findings meeting fail threshold '%s'", cfg.failOnSev)
		annot.Errorf("%s", msg)
		// Domain rule failure (scan threshold exceeded) — wrap so the CLI
		// exits with ExitCodeValidation (1), not ExitCodeSoftware (70).
		return fmt.Errorf("%s: %w", msg, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "OpenGrep completed successfully with %d findings\n", counts.FindingsTotal)

	return nil
}
