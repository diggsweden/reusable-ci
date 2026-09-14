// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	domainsecurity "github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

func reportFlag(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

func TestContainerFreshness_NoOldEvidenceOrPartialPublication(t *testing.T) { //nolint:gocognit // independent process/data/type outcomes share the same owned publication fixture.
	t.Parallel()

	cause := errors.New("owned scanner failure") //nolint:err113 // independent tool cause.

	for _, mode := range []string{"no-write", "empty", "malformed", "empty-object", "unknown-only", "status", "tool-error", "link", "oversized", "clean", "clean-empty-array", "clean-named-omitted", "clean-named-null", "findings"} {
		t.Run(mode, func(t *testing.T) {
			clean := strings.HasPrefix(mode, "clean")
			root := t.TempDir()

			raw, sarif, gitlab := filepath.Join(root, "raw.json"), filepath.Join(root, "out.sarif"), filepath.Join(root, "gitlab.json")
			for _, path := range []string{raw, sarif, gitlab} {
				require.NoError(t, os.WriteFile(path, []byte(`{"Results":[],"old":"OLD-CANARY"}`), 0600))
			}

			canary := filepath.Join(t.TempDir(), "canary")
			require.NoError(t, os.WriteFile(canary, []byte("outside unchanged"), 0600))

			var staged string

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				staged = scanOutput(args)
				require.NotEqual(t, raw, staged)
				require.True(t, filepath.IsAbs(staged))
				require.NoFileExists(t, staged)

				for _, path := range []string{raw, sarif, gitlab} {
					require.NoFileExists(t, path)
				}

				body := `{"ArtifactName":"current","Results":[]}`

				switch mode {
				case "no-write":
					return 0, nil
				case "tool-error":
					return 0, cause
				case "empty":
					body = ""
				case "malformed":
					body = "{"
				case "empty-object":
					body = `{}`
				case "unknown-only":
					body = `{"unexpected":true}`
				case "clean-empty-array":
					body = `{"Results":[]}`
				case "clean-named-omitted":
					body = `{"ArtifactName":"current"}`
				case "clean-named-null":
					body = `{"ArtifactName":"current","Results":null}`
				case "link":
					return 0, os.Symlink(canary, staged)
				case "oversized":
					f, err := os.Create(staged)
					require.NoError(t, err)
					require.NoError(t, f.Truncate((64<<20)+1))
					require.NoError(t, f.Close())

					return 0, nil
				case "findings":
					body = boundaryTrivyJSON
				}

				require.NoError(t, os.WriteFile(staged, []byte(body), 0600))

				if mode == "status" {
					return 7, nil
				}

				return 0, nil
			}}

			var out bytes.Buffer

			err := appsecurity.ScanContainer(t.Context(), scanner, &out, &out, output.Annotator{}, appsecurity.ScanContainerInput{ImageRef: "registry.invalid/owner/image:tag", JSONFile: raw, SARIFFile: sarif, GitLabReportFile: gitlab})
			want := errs.ErrMalformedInput

			switch mode {
			case "no-write":
				want = errs.ErrMissingInput
			case "status":
				want = errs.ErrDependencyUnavailable
			case "tool-error":
				want = cause
			case "clean", "clean-empty-array", "clean-named-omitted", "clean-named-null":
				want = nil
			case "findings":
				want = errs.ErrValidation
			}

			require.ErrorIs(t, err, want)
			require.Len(t, scanner.calls, 1)

			_, statErr := os.Stat(filepath.Dir(staged))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			body, readErr := os.ReadFile(canary)
			require.NoError(t, readErr)
			require.Equal(t, "outside unchanged", string(body))

			for _, path := range []string{raw, sarif, gitlab} {
				if mode == "findings" || clean && path == raw {
					body, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					require.True(t, json.Valid(body))
					require.NotContains(t, string(body), "OLD-CANARY")
				} else {
					require.NoFileExists(t, path)
				}
			}

			if clean {
				require.Contains(t, out.String(), "No vulnerabilities found")
			} else {
				require.NotContains(t, out.String(), "No vulnerabilities found")
			}
		})
	}
}

func TestDependencyFreshness_ReportEvidenceRequiredAtHEADAndBase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, phase, body string
		valid             bool
	}{
		{"empty-object", "head", `{}`, false},
		{"empty-object", "base", `{}`, false},
		{"unknown-only", "head", `{"unexpected":true}`, false},
		{"unknown-only", "base", `{"unexpected":true}`, false},
		{"empty-array", "head", `{"Results":[]}`, true},
		{"empty-array", "base", `{"Results":[]}`, true},
		{"named-omitted", "head", `{"ArtifactName":"current"}`, true},
		{"named-omitted", "base", `{"ArtifactName":"current"}`, true},
		{"named-null", "head", `{"ArtifactName":"current","Results":null}`, true},
		{"named-null", "base", `{"ArtifactName":"current","Results":null}`, true},
	} {
		t.Run(tc.phase+"/"+tc.name, func(t *testing.T) {
			root := t.TempDir()

			sarif, gitlab := filepath.Join(root, "scan.sarif"), filepath.Join(root, "scan.json")
			for _, path := range []string{sarif, gitlab} {
				require.NoError(t, os.WriteFile(path, []byte("OLD-CANARY"), 0o600))
			}

			calls := 0

			var workDir string

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				calls++
				path := scanOutput(args)
				workDir = filepath.Dir(path)
				require.NoFileExists(t, path)
				require.NoFileExists(t, sarif)
				require.NoFileExists(t, gitlab)

				body := `{"Results":[]}`
				if tc.phase == "head" && calls == 1 || tc.phase == "base" && calls == 2 {
					body = tc.body
				}

				if calls == 3 {
					require.True(t, tc.valid, "invalid primary report reached conversion")
					require.Equal(t, "convert", args[0])

					body = `{"version":"2.1.0","runs":[]}`
				} else {
					require.Equal(t, "fs", args[0])
				}

				return 0, os.WriteFile(path, []byte(body), 0o600)
			}}
			git := &comparisonGit{root: root}
			summary := &appSummaryBuf{}

			var out bytes.Buffer

			err := appsecurity.ScanDependencies(t.Context(), scanner, git, summary, &out, &out, output.Annotator{}, appsecurity.ScanDependenciesInput{ScanMode: domainsecurity.ScanModeDiff, ScanPath: root, BaseRef: "main", SARIFFile: sarif, GitLabDepFile: gitlab})
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, 3, calls)
				require.Contains(t, summary.buf.String(), "No new vulnerabilities")
				require.Contains(t, out.String(), "No new vulnerabilities found")

				for _, path := range []string{sarif, gitlab} {
					body, readErr := os.ReadFile(path)
					require.NoError(t, readErr)
					require.True(t, json.Valid(body))
					require.NotContains(t, string(body), "OLD-CANARY")
				}
			} else {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Contains(t, err.Error(), "extract "+tc.phase+" findings")
				require.Empty(t, summary.buf.String())
				require.NotContains(t, out.String(), "No new vulnerabilities found")
				require.NoFileExists(t, sarif)
				require.NoFileExists(t, gitlab)
				require.Equal(t, map[string]int{"head": 1, "base": 2}[tc.phase], calls)
			}

			wantGit := [][]string{{"rev-parse", "--show-toplevel"}}

			if tc.valid || tc.phase == "base" {
				worktree := filepath.Join(workDir, "base-worktree")
				wantGit = append(wantGit, []string{"worktree", "add", "-q", worktree, "origin/main"}, []string{"worktree", "remove", "-f", worktree})
			}

			require.Equal(t, wantGit, git.calls)

			_, statErr := os.Stat(workDir)
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestOpenGrepFreshness_ReportsBelongToCurrentInvocation(t *testing.T) { //nolint:gocognit // one four-report fixture checks independent raw/optional output and status cases.
	t.Parallel()

	for _, mode := range []string{"no-write", "status", "malformed", "clean", "findings", "optional-absent", "bad-sarif", "unreadable-text"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			in := appsecurity.RunOpengrepInput{TargetPath: root, JSONFile: filepath.Join(root, "raw.json"), SARIFFile: filepath.Join(root, "report.sarif"), TextFile: filepath.Join(root, "report.txt"), GitLabSASTFile: filepath.Join(root, "gitlab.json")}

			paths := []string{in.JSONFile, in.SARIFFile, in.TextFile, in.GitLabSASTFile}
			for _, path := range paths {
				require.NoError(t, os.WriteFile(path, []byte(`{"results":[],"old":"OLD-CANARY"}`), 0600))
			}

			var staged string

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				staged = reportFlag(args, "--json-output")
				require.NotEqual(t, in.JSONFile, staged)
				require.NoFileExists(t, staged)

				for _, path := range paths {
					require.NoFileExists(t, path)
				}

				if mode == "no-write" {
					return 0, nil
				}

				body := `{"results":[]}`
				if mode == "findings" {
					body = `{"results":[{"check_id":"current","extra":{"severity":"ERROR"}}]}`
				}

				if mode == "malformed" {
					body = "{"
				}

				require.NoError(t, os.WriteFile(staged, []byte(body), 0600))

				if mode == "optional-absent" {
					return 0, nil
				}

				sarif := `{"version":"2.1.0","runs":[]}`
				if mode == "bad-sarif" {
					sarif = "not-json"
				}

				require.NoError(t, os.WriteFile(reportFlag(args, "--sarif-output"), []byte(sarif), 0600))
				require.NoError(t, os.WriteFile(reportFlag(args, "--gitlab-sast-output"), []byte(`{"version":"15.0.4","vulnerabilities":[]}`), 0600))

				if mode == "unreadable-text" {
					require.NoError(t, os.Mkdir(reportFlag(args, "--text-output"), 0700))
				} else {
					require.NoError(t, os.WriteFile(reportFlag(args, "--text-output"), []byte("CURRENT TEXT"), 0600))
				}

				if mode == "status" {
					return 7, nil
				}

				return 0, nil
			}}
			sink := fakeoutputsink.New(t)
			require.NoError(t, sink.Set(t.Context(), "prior", "keep"))

			summary := &appSummaryBuf{}

			var out bytes.Buffer

			err := appsecurity.RunOpengrep(t.Context(), scanner, sink, summary, &out, &out, output.NewAnnotator(&out, output.FormatText), in)

			var want error

			switch mode {
			case "no-write":
				want = errs.ErrMissingInput
			case "status":
				want = errs.ErrDependencyUnavailable
			case "malformed", "bad-sarif":
				want = errs.ErrMalformedInput
			case "findings":
				want = errs.ErrValidation
			}

			require.ErrorIs(t, err, want)
			require.Len(t, scanner.calls, 1)
			require.NotContains(t, out.String()+summary.buf.String(), "OLD-CANARY")

			_, statErr := os.Stat(filepath.Dir(staged))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			if want != nil && mode != "findings" {
				require.Equal(t, map[string]string{"prior": "keep"}, sink.AllScalar())
				require.NotContains(t, out.String(), "completed successfully")
			}

			for _, path := range paths {
				body, readErr := os.ReadFile(path)
				if readErr == nil {
					require.NotContains(t, string(body), "OLD-CANARY")
				}
			}

			switch mode {
			case "no-write", "status", "malformed":
				for _, path := range paths {
					require.NoFileExists(t, path)
				}
			case "clean", "findings":
				for _, path := range paths {
					require.FileExists(t, path)
				}

				text, readErr := os.ReadFile(in.TextFile)
				require.NoError(t, readErr)
				require.Equal(t, "CURRENT TEXT", string(text))

				if mode == "findings" {
					require.Contains(t, summary.buf.String(), "CURRENT TEXT")
				}
			case "optional-absent":
				require.FileExists(t, in.JSONFile)

				for _, path := range paths[1:] {
					require.NoFileExists(t, path)
				}
			case "bad-sarif":
				require.NoFileExists(t, in.SARIFFile)
			case "unreadable-text":
				require.NoFileExists(t, in.TextFile)
				require.FileExists(t, in.JSONFile)
			}
		})
	}
}

func TestDependencyFreshness_ConversionFailureCannotPublishOldOrPartialSARIF(t *testing.T) { //nolint:gocognit // converter status/error/data variants must not mask report-publication assertions.
	t.Parallel()

	cause := errors.New("owned conversion failure") //nolint:err113 // distinct converter failure.

	for _, mode := range []string{"success", "no-write", "nonzero", "error", "malformed", "null", "head-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()

			sarif, gitlab := filepath.Join(root, "dep.sarif"), filepath.Join(root, "dep.json")
			for _, path := range []string{sarif, gitlab} {
				require.NoError(t, os.WriteFile(path, []byte("OLD-CANARY"), 0600))
			}

			var workDir string

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				path := scanOutput(args)
				workDir = filepath.Dir(path)
				require.NotEqual(t, sarif, path)
				require.NoFileExists(t, path)
				require.NoFileExists(t, sarif)
				require.NoFileExists(t, gitlab)

				if args[0] == "fs" {
					if mode == "head-failure" {
						return 9, nil
					}

					return 0, os.WriteFile(path, []byte(`{"Results":[]}`), 0600)
				}

				if mode == "no-write" {
					return 0, nil
				}

				body := `{"version":"2.1.0","runs":[]}`
				if mode == "malformed" {
					body = "{"
				}

				if mode == "null" {
					body = "null"
				}

				require.NoError(t, os.WriteFile(path, []byte(body), 0600))

				if mode == "error" {
					return 0, cause
				}

				if mode == "nonzero" {
					return 7, nil
				}

				return 0, nil
			}}

			var diagnostics bytes.Buffer

			summary := &appSummaryBuf{}

			err := appsecurity.ScanDependencies(t.Context(), scanner, &fakeGit{}, summary, io.Discard, io.Discard, output.NewAnnotator(&diagnostics, output.FormatText), appsecurity.ScanDependenciesInput{ScanMode: domainsecurity.ScanModeFull, ScanPath: root, SARIFFile: sarif, GitLabDepFile: gitlab})
			if mode == "head-failure" {
				require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
				require.Empty(t, summary.buf.String())
				require.NoFileExists(t, gitlab)
			} else {
				require.NoError(t, err)
				require.FileExists(t, gitlab)
				require.Contains(t, summary.buf.String(), "No new vulnerabilities")
			}

			if mode == "success" {
				require.FileExists(t, sarif)
			} else {
				require.NoFileExists(t, sarif)
			}

			if mode != "success" && mode != "head-failure" {
				switch mode {
				case "error":
					require.Contains(t, diagnostics.String(), cause.Error())
				case "nonzero":
					require.Contains(t, diagnostics.String(), "status 7")
				case "no-write":
					require.Contains(t, diagnostics.String(), "read fresh scan report")
				case "malformed":
					require.Contains(t, diagnostics.String(), "parse fresh JSON report")
				case "null":
					require.Contains(t, diagnostics.String(), "must be an object")
				}
			}

			_, statErr := os.Stat(workDir)
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestScanFreshness_InputAndOutputAliasesRefuseBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"same", "hardlink", "input-symlink", "cleaned-report-path"} {
		root := t.TempDir()
		input := filepath.Join(root, "source")
		report := input
		require.NoError(t, os.WriteFile(input, []byte("SOURCE-CANARY"), 0600))

		if kind == "hardlink" {
			report = filepath.Join(root, "report")
			require.NoError(t, os.Link(input, report))
		}

		if kind == "input-symlink" {
			input = filepath.Join(root, "link")
			require.NoError(t, os.Symlink(report, input))
		}

		if kind == "cleaned-report-path" {
			report = root + "/missing/../source"
		}

		scanner := &scanRecorder{run: func(context.Context, []string) (int, error) {
			t.Error("scanner called")

			return 0, nil
		}}
		err := appsecurity.RunOpengrep(t.Context(), scanner, fakeoutputsink.New(t), &appSummaryBuf{}, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{TargetPath: input, JSONFile: report, SARIFFile: filepath.Join(root, "s"), TextFile: filepath.Join(root, "t"), GitLabSASTFile: filepath.Join(root, "g")})
		require.ErrorIs(t, err, errs.ErrUsage)
		err = appsecurity.ScanDependencies(t.Context(), scanner, &fakeGit{}, &appSummaryBuf{}, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{ScanMode: domainsecurity.ScanModeFull, ScanPath: input, SARIFFile: report, GitLabDepFile: filepath.Join(root, "g")})
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, scanner.calls)

		body, err := os.ReadFile(filepath.Clean(report))
		require.NoError(t, err)
		require.Equal(t, "SOURCE-CANARY", string(body))
	}
}

func TestScanFreshness_InvalidPathsPreservePreviousReports(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"alias", "trailing-separator"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			raw := filepath.Join(root, "raw.json")
			require.NoError(t, os.WriteFile(raw, []byte("OLD-CANARY"), 0600))

			other := filepath.Join(root, "report.sarif")
			require.NoError(t, os.WriteFile(other, []byte("OTHER-CANARY"), 0o600))

			report := raw
			if mode == "trailing-separator" {
				report = other + string(filepath.Separator)
			}

			scanner := &scanRecorder{run: func(context.Context, []string) (int, error) {
				t.Error("scanner called")

				return 0, nil
			}}
			err := appsecurity.ScanContainer(t.Context(), scanner, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanContainerInput{ImageRef: "image", JSONFile: raw, SARIFFile: report, GitLabReportFile: filepath.Join(root, "g")})
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Empty(t, scanner.calls)

			body, err := os.ReadFile(raw)
			require.NoError(t, err)
			require.Equal(t, "OLD-CANARY", string(body))
			body, err = os.ReadFile(other)
			require.NoError(t, err)
			require.Equal(t, "OTHER-CANARY", string(body))
		})
	}
}

func TestDependencyFreshness_PrimaryEmptyMissingAndError(t *testing.T) {
	t.Parallel()

	cause := errors.New("owned primary scan failure") //nolint:err113 // exact dependency cause.

	for _, mode := range []string{"head-empty", "base-empty", "head-missing", "base-missing", "head-error", "base-error"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()

			sarif, gitlab := filepath.Join(root, "scan.sarif"), filepath.Join(root, "scan.json")
			for _, path := range []string{sarif, gitlab} {
				require.NoError(t, os.WriteFile(path, []byte("OLD-CANARY"), 0600))
			}

			calls := 0

			var workDir string

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				calls++

				require.Equal(t, "fs", args[0])
				path := scanOutput(args)
				workDir = filepath.Dir(path)

				fail := calls == 1 && (mode == "head-empty" || mode == "head-missing" || mode == "head-error") || calls == 2
				if fail {
					switch mode {
					case "head-error", "base-error":
						return 0, cause
					case "head-missing", "base-missing":
						return 0, nil
					default:
						return 0, os.WriteFile(path, nil, 0600)
					}
				}

				return 0, os.WriteFile(path, []byte(`{"Results":[]}`), 0600)
			}}
			summary := &appSummaryBuf{}

			var out bytes.Buffer

			err := appsecurity.ScanDependencies(t.Context(), scanner, &comparisonGit{root: root}, summary, &out, &out, output.Annotator{}, appsecurity.ScanDependenciesInput{ScanMode: domainsecurity.ScanModeDiff, ScanPath: root, BaseRef: "main", SARIFFile: sarif, GitLabDepFile: gitlab})
			want := errs.ErrMalformedInput

			switch mode {
			case "head-error", "base-error":
				want = cause
			case "head-missing", "base-missing":
				want = errs.ErrMissingInput
			}

			require.ErrorIs(t, err, want)
			require.Empty(t, summary.buf.String())
			require.NotContains(t, out.String(), "No new vulnerabilities found")

			if mode == "head-empty" || mode == "head-missing" || mode == "head-error" {
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, 2, calls)
			}

			require.NoFileExists(t, sarif)
			require.NoFileExists(t, gitlab)

			_, statErr := os.Stat(workDir)
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestScanFreshness_ConfigAndRuleAliasesPreserveInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rules := filepath.Join(root, "rules.yml")
	require.NoError(t, os.WriteFile(rules, []byte("RULES-CANARY"), 0600))

	scanner := &scanRecorder{run: func(context.Context, []string) (int, error) {
		t.Error("scanner called")

		return 0, nil
	}}
	err := appsecurity.RunOpengrep(t.Context(), scanner, fakeoutputsink.New(t), &appSummaryBuf{}, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{TargetPath: root, Config: "p/default," + rules, JSONFile: rules, SARIFFile: filepath.Join(root, "s"), TextFile: filepath.Join(root, "t"), GitLabSASTFile: filepath.Join(root, "g")})
	require.ErrorIs(t, err, errs.ErrUsage)

	for _, severity := range []string{"INVALID", "HIGH,", "HIGH\nLOW"} {
		err = appsecurity.ScanContainer(t.Context(), scanner, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanContainerInput{ImageRef: "image", Severity: severity, JSONFile: rules, SARIFFile: filepath.Join(root, "s"), GitLabReportFile: filepath.Join(root, "g")})
		require.ErrorIs(t, err, errs.ErrUsage)
	}

	err = appsecurity.ScanDependencies(t.Context(), scanner, &fakeGit{}, &appSummaryBuf{}, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{ScanMode: "INVALID", ScanPath: root, SARIFFile: rules, GitLabDepFile: filepath.Join(root, "g")})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Empty(t, scanner.calls)

	body, readErr := os.ReadFile(rules)
	require.NoError(t, readErr)
	require.Equal(t, "RULES-CANARY", string(body))
}

func TestScanFreshness_SupportedSeverityAndInputLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	link := filepath.Join(root, "link")

	require.NoError(t, os.WriteFile(source, []byte("SOURCE-CANARY"), 0600))
	require.NoError(t, os.Symlink(source, link))
	in := appsecurity.RunOpengrepInput{TargetPath: link, JSONFile: filepath.Join(root, "raw.json"), SARIFFile: filepath.Join(root, "sarif"), TextFile: filepath.Join(root, "text"), GitLabSASTFile: filepath.Join(root, "gitlab")}
	require.NoError(t, appsecurity.RunOpengrep(t.Context(), &fakeOpengrep{writeJSON: `{"results":[]}`}, fakeoutputsink.New(t), &appSummaryBuf{}, io.Discard, io.Discard, output.Annotator{}, in))

	scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
		require.Equal(t, "UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL", reportFlag(args, "--severity"))

		return 0, os.WriteFile(scanOutput(args), []byte(`{"ArtifactName":"clean-native-report"}`), 0600)
	}}
	require.NoError(t, appsecurity.ScanContainer(t.Context(), scanner, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanContainerInput{ImageRef: "image", Severity: "unknown, low,medium,HIGH,critical", JSONFile: in.JSONFile, SARIFFile: in.SARIFFile, GitLabReportFile: in.GitLabSASTFile}))

	body, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "SOURCE-CANARY", string(body))
}
