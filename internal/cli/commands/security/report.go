// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
)

// Shared flag names for the report converters.
const (
	flagInput  = "input"
	flagOutput = "output"
)

// reportGroup wires `reusable-ci security report <subcmd>` — every
// subcommand here transforms or uploads an existing security report
// produced by a prior `security scan ...` step.
func reportGroup() *cli.Command {
	return &cli.Command{
		Name:  "report",
		Usage: "transform or upload a security scan report",
		Commands: []*cli.Command{
			reportEnrichSARIFCmd(),
			reportUploadSARIFCmd(),
			reportToGitLabDepCmd(),
			reportToGitLabContainerCmd(),
			reportToGitLabSASTCmd(),
		},
	}
}

func reportEnrichSARIFCmd() *cli.Command {
	return &cli.Command{
		Name:  "enrich-sarif",
		Usage: "populate partialFingerprints.primaryLocationLineHash on every SARIF result for GitHub Code Scanning dedupe",
		Description: `EXAMPLE:
   reusable-ci security report enrich-sarif --sarif-file results.sarif`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("SARIF_FILE"), Usage: "SARIF file to read, enrich, and write back (\"-\" reads stdin)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			return appsecurity.EnrichGitHubSARIFFile(os.Stderr, os.Stderr, annot, appsecurity.EnrichGitHubSARIFInput{
				Path: cmd.String("sarif-file"),
			})
		},
	}
}

func reportUploadSARIFCmd() *cli.Command {
	return &cli.Command{
		Name:  "upload-sarif",
		Usage: "upload a SARIF file to the platform's code-scanning surface (GitHub Code Scanning; skipped on GitLab/Forgejo/local, which have no SARIF ingestion)",
		Description: `EXAMPLE:
   reusable-ci security report upload-sarif --sarif-file results.sarif --category opengrep`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "sarif-file", Sources: cli.EnvVars("SARIF_FILE"), Usage: "SARIF file to upload to Code Scanning (\"-\" reads stdin)"},
			&cli.StringFlag{
				Name:  "token-file",
				Usage: "path to a file containing the code-scanning token (use \"-\" for stdin; defaults to $CODE_SCANNING_TOKEN)",
			},
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "\"owner/repo\" the SARIF findings are attributed to"},
			&cli.StringFlag{Name: "commit", Sources: cienv.Commit(), Usage: "commit SHA the SARIF findings are attributed to"},
			&cli.StringFlag{Name: "ref", Sources: cienv.Ref(), Usage: "fully-qualified ref (refs/heads/X or refs/tags/X) for attribution"},
			&cli.StringFlag{Name: "category", Sources: cli.EnvVars("SARIF_CATEGORY"), Usage: "Code Scanning category label (groups multi-scanner results)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			token, err := secret.Resolve(cmd.String("token-file"), "CODE_SCANNING_TOKEN")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				up, err := d.RequireSARIFUploader()
				if err != nil {
					// Non-fatal: SARIF upload is GitHub-only; on
					// GitLab/local the workflow continues and the
					// platform-native security-report path (gl-sast)
					// handles findings.
					annot.Noticef("SARIF upload skipped — %v", err)

					return nil
				}

				return appsecurity.UploadSARIF(ctx, up, os.Stderr, annot, appsecurity.UploadSARIFInput{
					SARIFFile:  cmd.String("sarif-file"),
					Token:      token,
					Repository: cmd.String("repository"),
					SHA:        cmd.String("commit"),
					Ref:        cmd.String("ref"),
					Category:   cmd.String("category"),
				})
			})
		},
	}
}

func reportToGitLabDepCmd() *cli.Command {
	return &cli.Command{
		Name:  "to-gitlab-dep",
		Usage: "convert Trivy JSON to a GitLab dependency-scanning report",
		Description: `EXAMPLE:
   reusable-ci security report to-gitlab-dep --input trivy.json --output gl-dependency-scanning.json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagInput,
				Required: true,
				Sources:  cli.EnvVars("INPUT_FILE"),
				Usage:    "Trivy JSON report to convert",
			},
			&cli.StringFlag{
				Name:     flagOutput,
				Required: true,
				Sources:  cli.EnvVars("OUTPUT_FILE"),
				Usage:    "destination path for the GitLab dependency-scanning report",
			},
			&cli.StringFlag{
				Name:    "trivy-version", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources: cli.EnvVars("TRIVY_VERSION"),
				Usage:   "version string to embed in scan.scanner.version (default: unknown)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String(flagOutput)

			n, err := appsecurity.TrivyToGitLabDep(appsecurity.TransformInput{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				InputPath:    cmd.String(flagInput),
				OutputPath:   out,
				TrivyVersion: cmd.String("trivy-version"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "Wrote GitLab dependency-scanning report: %s (%d findings)\n", out, n)

			return nil
		},
	}
}

func reportToGitLabContainerCmd() *cli.Command {
	return &cli.Command{
		Name:  "to-gitlab-container",
		Usage: "convert Trivy JSON to a GitLab container-scanning report",
		Description: `EXAMPLE:
   reusable-ci security report to-gitlab-container --input trivy.json --output gl-container-scanning.json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagInput,
				Required: true,
				Sources:  cli.EnvVars("INPUT_FILE"),
				Usage:    "Trivy JSON report to convert",
			},
			&cli.StringFlag{
				Name:     flagOutput,
				Required: true,
				Sources:  cli.EnvVars("OUTPUT_FILE"),
				Usage:    "destination path for the GitLab container-scanning report",
			},
			&cli.StringFlag{
				Name:    flagImageRef,
				Sources: cli.EnvVars("IMAGE_REF"),
				Usage:   "fully qualified image (registry/owner/name@sha256:…); falls back to ArtifactName from the Trivy JSON when empty",
			},
			&cli.StringFlag{
				Name:    "trivy-version",
				Sources: cli.EnvVars("TRIVY_VERSION"),
				Usage:   "Trivy version embedded in scan.scanner.version of the GitLab report (default: unknown)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String(flagOutput)

			n, err := appsecurity.TrivyToGitLabContainer(appsecurity.TransformInput{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				InputPath:    cmd.String(flagInput),
				OutputPath:   out,
				TrivyVersion: cmd.String("trivy-version"),
				ImageRef:     cmd.String(flagImageRef),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "Wrote GitLab container-scanning report: %s (%d findings)\n", out, n)

			return nil
		},
	}
}

func reportToGitLabSASTCmd() *cli.Command {
	return &cli.Command{
		Name:  "to-gitlab-sast",
		Usage: "convert a generic SARIF file to a GitLab SAST report",
		Description: `Converts a generic SARIF v2.1.0 document (e.g. from nanolinter) into the
   GitLab SAST report schema so a merge request's Security tab is populated via
   artifacts:reports:sast. The scanner identity is read from the SARIF
   tool.driver; severity comes from properties.security-severity (CVSS) when
   present, otherwise from the SARIF level.

EXAMPLE:
   reusable-ci security report to-gitlab-sast --input results.sarif --output gl-sast-report.json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagInput,
				Required: true,
				Sources:  cli.EnvVars("INPUT_FILE"),
				Usage:    "SARIF report to convert",
			},
			&cli.StringFlag{
				Name:     flagOutput,
				Required: true,
				Sources:  cli.EnvVars("OUTPUT_FILE"),
				Usage:    "destination path for the GitLab SAST report",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			out := cmd.String(flagOutput)

			n, err := appsecurity.SARIFToGitLabSAST(appsecurity.TransformInput{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				InputPath:  cmd.String(flagInput),
				OutputPath: out,
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "Wrote GitLab SAST report: %s (%d findings)\n", out, n)

			return nil
		},
	}
}
