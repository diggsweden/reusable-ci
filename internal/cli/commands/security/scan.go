// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"context"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/opengrep"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/trivy"
	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// flagFailOnSeverity is the shared name of the severity-gate flag across
// every `security scan` subcommand (container/dependencies/opengrep), so
// the fail threshold reads the same regardless of scanner.
const flagFailOnSeverity = "fail-on-severity"

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
			scanContainerJSONCmd(),
		},
	}
}

func scanOpengrepCmd() *cli.Command {
	return &cli.Command{
		Name:  "opengrep",
		Usage: "run an opengrep SAST scan, emit findings + JSON/SARIF/text/GitLab-SAST artifacts, write step summary",
		Description: `EXAMPLE:
   reusable-ci security scan opengrep --config "p/owasp-top-ten,p/cwe" --fail-on-severity error`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Sources: cli.EnvVars("OPENGREP_CONFIG"), Usage: "comma/space/newline-separated opengrep rulesets (e.g. \"p/owasp-top-ten,p/cwe\")"},
			&cli.StringFlag{Name: flagFailOnSeverity, Sources: cli.EnvVars("OPENGREP_FAIL_ON_SEVERITY"), Usage: "minimum severity that makes the scan exit non-zero (info/warning/error)"},
			&cli.StringFlag{Name: "target-path", Sources: cli.EnvVars("OPENGREP_TARGET_PATH"), Usage: "path the scanner walks (default: cwd)"},
			&cli.StringFlag{Name: "json-file", Sources: cli.EnvVars("OPENGREP_JSON_FILE"), Usage: "destination path for the JSON findings file"},
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("OPENGREP_SARIF_FILE"), Usage: "destination path for the SARIF findings file"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "text-file", Sources: cli.EnvVars("OPENGREP_TEXT_FILE"), Usage: "destination path for the human-readable text findings file"},
			&cli.StringFlag{Name: "gitlab-sast-file", Sources: cli.EnvVars("OPENGREP_GITLAB_SAST_FILE"), Usage: "destination path for the GitLab SAST report"},
			&cli.BoolFlag{Name: "has-code-scanning-token", Sources: cli.EnvVars("HAS_CODE_SCANNING_TOKEN"), Usage: "code-scanning upload token is available (toggles step-summary upload section)"},
			&cli.StringFlag{Name: "run-url", Sources: cienv.RunURL(), Usage: "CI run URL emitted in the step summary for the linked findings"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				annot := deps.Annotator(cmd)

				return appsecurity.RunOpengrep(ctx, opengrep.New(), d.OutputSink, d.SummarySink, os.Stderr, os.Stderr, annot, appsecurity.RunOpengrepInput{
					Config:             cmd.String("config"),
					FailOnSeverity:     cmd.String(flagFailOnSeverity),
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
		Description: `EXAMPLE:
   reusable-ci security scan dependencies --fail-on-severity high --scan-mode full --scan-path .`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFailOnSeverity, Value: string(security.DepSeverityCritical), Sources: cli.EnvVars("DEPENDENCIES_FAIL_ON_SEVERITY"), Usage: "minimum severity that fails the scan (low/moderate/high/critical)"},
			&cli.StringFlag{Name: "scan-mode", Value: "diff", Sources: cli.EnvVars("DEPENDENCIES_SCAN_MODE"), Usage: "diff (vs base ref) or full (entire workspace)"},
			&cli.StringFlag{Name: "scan-path", Value: ".", Sources: cli.EnvVars("DEPENDENCIES_SCAN_PATH"), Usage: "directory trivy scans"},
			&cli.StringFlag{Name: "base-ref", Sources: cli.EnvVars("CI_PR_BASE_REF"), Usage: "base ref the diff scan compares against"},
			&cli.StringFlag{Name: "sarif-file", Value: security.DefaultTrivySARIFFile, Sources: cli.EnvVars("DEPENDENCIES_SARIF_FILE"), Usage: "destination path for the trivy SARIF findings"},
			&cli.StringFlag{Name: "gitlab-dep-file", Value: security.DefaultTrivyGitLabDepFile, Sources: cli.EnvVars("DEPENDENCIES_GITLAB_FILE"), Usage: "destination path for the GitLab dependency-scanning report"},
			&cli.StringFlag{Name: "trivy-version", Sources: cli.EnvVars("TRIVY_VERSION"), Usage: "trivy version string embedded in the SARIF / GitLab report"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appsecurity.ScanDependencies(ctx, trivy.New(), git.New(), d.SummarySink, os.Stderr, os.Stderr, annot, appsecurity.ScanDependenciesInput{
					FailOnSeverity: cmd.String(flagFailOnSeverity),
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
		Description: `EXAMPLES:
   # Scan a digest-pinned image, failing on HIGH+ findings
   reusable-ci security scan container \
     --image-ref ghcr.io/owner/app@sha256:… --fail-on-severity HIGH

   # Write the trivy JSON for a downstream step
   reusable-ci security scan container --image-ref ghcr.io/owner/app:v1.2.3 \
     --json-file dist/trivy.json`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagImageRef, Required: true, Sources: cli.EnvVars("IMAGE_REF"), Usage: "fully-qualified image (registry/owner/name@sha256:…) to scan"},
			&cli.StringFlag{Name: "json-file", Value: security.DefaultTrivyContainerJSONFile, Sources: cli.EnvVars("CONTAINER_JSON_FILE"), Usage: "destination path for the raw trivy JSON findings"},
			&cli.StringFlag{Name: "sarif-file", Value: security.DefaultTrivyContainerSARIFFile, Sources: cli.EnvVars("CONTAINER_SARIF_FILE"), Usage: "destination path for the trivy SARIF findings"},
			&cli.StringFlag{Name: "gitlab-report-file", Value: security.DefaultTrivyGitLabContainerFile, Sources: cli.EnvVars("CONTAINER_GITLAB_FILE"), Usage: "destination path for the GitLab container-scanning report"},
			&cli.StringFlag{Name: flagFailOnSeverity, Value: "CRITICAL,HIGH", Sources: cli.EnvVars("CONTAINER_FAIL_ON_SEVERITY"), Usage: "trivy --severity filter that also sets the fail threshold; any finding at this level or above fails the scan (comma-list, e.g. 'CRITICAL,HIGH'; narrow to 'CRITICAL' to relax)"},
			&cli.StringFlag{Name: "trivy-version", Sources: cli.EnvVars("TRIVY_VERSION"), Usage: "trivy version string embedded in the SARIF / GitLab report"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appsecurity.ScanContainer(ctx, trivy.New(), os.Stderr, os.Stderr, annot, appsecurity.ScanContainerInput{
				ImageRef:         cmd.String(flagImageRef),
				JSONFile:         cmd.String("json-file"),
				SARIFFile:        cmd.String("sarif-file"),
				GitLabReportFile: cmd.String("gitlab-report-file"),
				Severity:         cmd.String(flagFailOnSeverity),
				TrivyVersion:     cmd.String("trivy-version"),
			})
		},
	}
}

func scanContainerJSONCmd() *cli.Command {
	return &cli.Command{
		Name:  "container-json",
		Usage: "run `trivy image`, retry transient failures, and validate raw JSON output shape",
		Description: `Runs Trivy against one image/platform and validates that the raw
JSON output is an object with a top-level Results array. It does not fail on
vulnerability findings or derive SARIF/GitLab reports; use ` + "`security scan container`" + ` for a
severity-gated scanner.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagImageRef, Required: true, Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_REF", "IMAGE_REF"), Usage: "image reference to scan"},
			&cli.StringFlag{Name: "platform", Value: "linux/amd64", Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_PLATFORM"), Usage: "OCI platform to scan, for example linux/amd64 or linux/arm64"},
			&cli.StringFlag{Name: "output", Required: true, Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_OUTPUT"), Usage: "destination path for the Trivy JSON result"},
			&cli.StringFlag{Name: "timeout", Value: "30m", Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_TIMEOUT"), Usage: "Trivy scan timeout"},
			&cli.IntFlag{Name: "attempts", Value: 3, Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_ATTEMPTS"), Usage: "scan attempts before failing"},
			&cli.IntFlag{Name: "retry-delay-seconds", Value: 20, Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_RETRY_DELAY_SECONDS"), Usage: "base delay between scan retry attempts"},
			&cli.StringFlag{Name: "scanners", Value: "vuln", Sources: cli.EnvVars("SCAN_CONTAINER_IMAGE_SCANNERS"), Usage: "comma-separated Trivy scanners"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsecurity.RawContainerScan(ctx, trivy.New(), d.OutputSink, os.Stderr, os.Stderr, appsecurity.RawContainerScanInput{
					ImageRef:   cmd.String(flagImageRef),
					Platform:   cmd.String("platform"),
					Output:     cmd.String("output"),
					Timeout:    cmd.String("timeout"),
					Attempts:   cmd.Int("attempts"),
					RetryDelay: time.Duration(cmd.Int("retry-delay-seconds")) * time.Second,
					Scanners:   cmd.String("scanners"),
				})
			})
		},
	}
}
