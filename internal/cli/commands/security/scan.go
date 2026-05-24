// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/opengrep"
	"github.com/diggsweden/reusable-ci/internal/adapters/trivy"
	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// scanGroup wires `reusable-ci security scan <subcmd>` — every
// subcommand here invokes a scanner against the workspace or an
// image. Subcommands write findings + secondary report artefacts
// (SARIF, GitLab SAST / container / dep schemas) for downstream
// `security report ...` steps to upload or convert.
func scanGroup() *cli.Command {
	return &cli.Command{
		Name:  "scan",
		Usage: "run a security scanner against the workspace or an image",
		Commands: []*cli.Command{
			scanOpengrepCmd(),
			scanDependenciesCmd(),
			scanContainerCmd(),
		},
	}
}

func scanOpengrepCmd() *cli.Command {
	return &cli.Command{
		Name:  "opengrep",
		Usage: "run an opengrep SAST scan, emit findings + JSON/SARIF/text/GitLab-SAST artifacts, write step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Sources: cli.EnvVars("OPENGREP_CONFIG"), Usage: "comma-separated opengrep rulesets (e.g. \"p/owasp-top-ten,p/cwe\")"},
			&cli.StringFlag{Name: "fail-on-severity", Sources: cli.EnvVars("OPENGREP_FAIL_ON_SEVERITY"), Usage: "minimum severity that makes the scan exit non-zero (info/warning/error)"},
			&cli.StringFlag{Name: "target-path", Sources: cli.EnvVars("OPENGREP_TARGET_PATH"), Usage: "path the scanner walks (default: cwd)"},
			&cli.StringFlag{Name: "json-file", Sources: cli.EnvVars("OPENGREP_JSON_FILE"), Usage: "destination path for the JSON findings file"},
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("OPENGREP_SARIF_FILE"), Usage: "destination path for the SARIF findings file"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "text-file", Sources: cli.EnvVars("OPENGREP_TEXT_FILE"), Usage: "destination path for the human-readable text findings file"},
			&cli.StringFlag{Name: "gitlab-sast-file", Sources: cli.EnvVars("OPENGREP_GITLAB_SAST_FILE"), Usage: "destination path for the GitLab SAST report"},
			&cli.BoolFlag{Name: "has-code-scanning-token", Sources: cli.EnvVars("HAS_CODE_SCANNING_TOKEN"), Usage: "code-scanning upload token is available (toggles step-summary upload section)"},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL"), Usage: "CI run URL emitted in the step summary for the linked findings"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				annot := deps.Annotator(cmd)

				return appsecurity.RunOpengrep(ctx, opengrep.New(), d.OutputSink, d.SummarySink, os.Stderr, os.Stderr, annot, appsecurity.RunOpengrepInput{
					Config:             cmd.String("config"),
					FailOnSeverity:     cmd.String("fail-on-severity"),
					TargetPath:         cmd.String("target-path"),
					JSONFile:           cmd.String("json-file"),
					SARIFFile:          cmd.String("sarif-file"),
					TextFile:           cmd.String("text-file"),
					GitLabSASTFile:     cmd.String("gitlab-sast-file"),
					Platform:           d.Platform,
					HasCodeScanningTok: cmd.Bool("has-code-scanning-token") || os.Getenv("CODE_SCANNING_TOKEN") != "",
					RunURL:             cmd.String("run-url"),
				})
			})
		},
	}
}

func scanDependenciesCmd() *cli.Command {
	return &cli.Command{
		Name:  "dependencies",
		Usage: "scan project dependencies for known vulnerabilities (Trivy, diff-mode against base ref)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "fail-on-severity", Value: string(security.DepSeverityCritical), Sources: cli.EnvVars("FAIL_ON_SEVERITY"), Usage: "minimum severity that fails the scan (low/moderate/high/critical)"},
			&cli.StringFlag{Name: "scan-mode", Value: "diff", Sources: cli.EnvVars("SCAN_MODE"), Usage: "diff (vs base ref) or full (entire workspace)"},
			&cli.StringFlag{Name: "scan-path", Value: ".", Sources: cli.EnvVars("SCAN_PATH"), Usage: "directory trivy scans"},
			&cli.StringFlag{Name: "base-ref", Sources: cli.EnvVars("CI_PR_BASE_REF"), Usage: "base ref the diff scan compares against"},
			&cli.StringFlag{Name: "sarif-file", Value: security.DefaultTrivySARIFFile, Usage: "destination path for the trivy SARIF findings"},
			&cli.StringFlag{Name: "gitlab-dep-file", Value: security.DefaultTrivyGitLabDepFile, Usage: "destination path for the GitLab dependency-scanning report"},
			&cli.StringFlag{Name: "trivy-version", Sources: cli.EnvVars("TRIVY_VERSION"), Usage: "trivy version string embedded in the SARIF / GitLab report"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appsecurity.ScanDependencies(ctx, trivy.New(), git.New(), d.SummarySink, os.Stderr, os.Stderr, annot, appsecurity.ScanDependenciesInput{
					FailOnSeverity: cmd.String("fail-on-severity"),
					ScanMode:       security.ScanMode(cmd.String("scan-mode")),
					ScanPath:       cmd.String("scan-path"),
					BaseRef:        cmd.String("base-ref"),
					SARIFFile:      cmd.String("sarif-file"),
					GitLabDepFile:  cmd.String("gitlab-dep-file"),
					TrivyVersion:   cmd.String("trivy-version"),
				})
			})
		},
	}
}

func scanContainerCmd() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "run `trivy image`, derive SARIF/GitLab reports, and fail when findings hit the severity threshold",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "image-ref", Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "fully-qualified image (registry/owner/name@sha256:…) to scan"},
			&cli.StringFlag{Name: "json-file", Value: security.DefaultTrivyContainerJSONFile, Sources: cli.EnvVars("TRIVY_JSON_FILE"), Usage: "destination path for the raw trivy JSON findings"},
			&cli.StringFlag{Name: "sarif-file", Value: security.DefaultTrivyContainerSARIFFile, Sources: cli.EnvVars("TRIVY_SARIF_FILE"), Usage: "destination path for the trivy SARIF findings"},
			&cli.StringFlag{Name: "gitlab-report-file", Value: security.DefaultTrivyGitLabContainerFile, Sources: cli.EnvVars("GITLAB_CONTAINER_SCAN_FILE"), Usage: "destination path for the GitLab container-scanning report"},
			&cli.StringFlag{Name: "severity", Value: "CRITICAL,HIGH", Sources: cli.EnvVars("TRIVY_SEVERITY"), Usage: "trivy --severity filter AND fail-on threshold; any finding at this level or above fails the scan (narrow to e.g. 'CRITICAL' to relax the gate)"},
			&cli.StringFlag{Name: "trivy-version", Sources: cli.EnvVars("TRIVY_VERSION"), Usage: "trivy version string embedded in the SARIF / GitLab report"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appsecurity.ScanContainer(ctx, trivy.New(), os.Stderr, os.Stderr, annot, appsecurity.ScanContainerInput{
				ImageRef:         cmd.String("image-ref"),
				JSONFile:         cmd.String("json-file"),
				SARIFFile:        cmd.String("sarif-file"),
				GitLabReportFile: cmd.String("gitlab-report-file"),
				Severity:         cmd.String("severity"),
				TrivyVersion:     cmd.String("trivy-version"),
			})
		},
	}
}
