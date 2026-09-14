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
	"strconv"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
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
	Platform           provider.ForgeAPI // passed to summary platform context
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
//
//nolint:cyclop // config/input/report preflight and independent scan/publication outcomes remain explicit.
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

	if aliasErr := validateScanInputReports(cfg.targetPath, cfg.jsonFile, cfg.sarifFile, cfg.textFile, cfg.gitlabFile); aliasErr != nil {
		return aliasErr
	}

	for _, rules := range cfg.configList {
		if aliasErr := validateScanInputReports(rules, cfg.jsonFile, cfg.sarifFile, cfg.textFile, cfg.gitlabFile); aliasErr != nil {
			return aliasErr
		}
	}

	workDir, err := prepareScanReports(cfg.jsonFile, cfg.sarifFile, cfg.textFile, cfg.gitlabFile)
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(workDir) }()

	destination := cfg
	cfg.jsonFile, cfg.sarifFile, cfg.textFile, cfg.gitlabFile = filepath.Join(workDir, "raw.json"), filepath.Join(workDir, "report.sarif"), filepath.Join(workDir, "report.txt"), filepath.Join(workDir, "gitlab.json")

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

	if publishErr := publishOpengrepReports(cfg, destination, annot); publishErr != nil {
		return publishErr
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

func publishOpengrepReports(source, destination opengrepConfig, annot output.Annotator) error {
	if err := publishScanReport(source.jsonFile, destination.jsonFile, true); err != nil {
		return err
	}

	for _, report := range []struct {
		source, destination string
		json                bool
	}{
		{source.sarifFile, destination.sarifFile, true}, {source.gitlabFile, destination.gitlabFile, true}, {source.textFile, destination.textFile, false},
	} {
		if err := publishScanReport(report.source, report.destination, report.json); err != nil && !errors.Is(err, os.ErrNotExist) {
			if report.json {
				return err
			}

			annot.Warningf("publish OpenGrep text report: %v", err)
		}
	}

	return nil
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
	jsonBody, err := readScanReport(cfg.jsonFile)
	if err != nil {
		return security.OpengrepCounts{}, "", fmt.Errorf("read opengrep json output %s: %w", cfg.jsonFile, err)
	}

	counts, err := security.CountOpengrepFindings(string(jsonBody))
	if err != nil {
		return security.OpengrepCounts{}, "", err
	}

	textBody, _ := readScanReport(cfg.textFile)
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
