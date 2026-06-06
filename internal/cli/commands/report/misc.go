// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package report

import (
	"context"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// One-offs that don't fit any of the typed subgroups but write a
// step-summary block of their own.

func extractedBinariesCmd() *cli.Command {
	return &cli.Command{
		Name:  "extracted-binaries",
		Usage: "append the extracted container binaries summary block",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Value: "./extracted-binaries", Sources: cli.EnvVars("EXTRACTED_BINARIES_DIR"), Usage: "directory containing the extracted binaries (scanned recursively)"},
			&cli.StringFlag{Name: "artifact-name", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "project slug shown in the step-summary header"},
			&cli.StringFlag{Name: "display-name", Sources: cli.EnvVars("DISPLAY_NAME"), Usage: "human-readable name override for the summary header"},
			&cli.StringFlag{Name: "extract-target", Sources: cli.EnvVars("EXTRACT_TARGET"), Usage: "Containerfile stage name the binaries were extracted from"},
			&cli.StringFlag{Name: "expected-names", Sources: cli.EnvVars("EXPECTED_NAMES"), Usage: "comma-separated allow-list of expected binary base names"},
			&cli.StringFlag{Name: "platform", Sources: cli.EnvVars("PLATFORM"), Usage: "build platform the binaries belong to (e.g. linux/amd64)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.IntFlag{Name: "limit", Value: 50, Sources: cli.EnvVars("FILE_LIST_LIMIT"), Usage: "maximum number of binary entries listed in the summary"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.ExtractedBinaries(ctx, d.SummarySink, appsummary.ExtractedBinariesInput{
					Dir:           cmd.String("dir"),
					ArtifactName:  cmd.String("artifact-name"),
					DisplayName:   cmd.String("display-name"),
					ExtractTarget: cmd.String("extract-target"),
					ExpectedNames: cmd.String("expected-names"),
					Platform:      cmd.String("platform"),
					Limit:         cmd.Int("limit"),
				})
			})
		},
	}
}

func swiftLintCmd() *cli.Command {
	return &cli.Command{
		Name:  "swift-lint",
		Usage: "write the aggregated Swift lint table to the step summary and exit non-zero when an enabled linter failed",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "enable-swiftformat", Sources: cli.EnvVars("ENABLE_SWIFTFORMAT"), Usage: "swift-format was enabled in the gate (toggles its summary row)"},
			&cli.StringFlag{Name: "swiftformat-result", Sources: cli.EnvVars("SWIFTFORMAT_RESULT"), Usage: "swift-format step outcome (success/failure/skipped)"},
			&cli.BoolFlag{Name: "enable-swiftlint", Sources: cli.EnvVars("ENABLE_SWIFTLINT"), Usage: "swiftlint was enabled in the gate (toggles its summary row)"},
			&cli.StringFlag{Name: "swiftlint-result", Sources: cli.EnvVars("SWIFTLINT_RESULT"), Usage: "swiftlint step outcome (success/failure/skipped)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.SwiftLintSummary(ctx, d.SummarySink, appsummary.SwiftLintSummaryInput{
					SwiftFormatEnabled: cmd.Bool("enable-swiftformat"),
					SwiftFormatResult:  cmd.String("swiftformat-result"),
					SwiftLintEnabled:   cmd.Bool("enable-swiftlint"),
					SwiftLintResult:    cmd.String("swiftlint-result"),
				})
			})
		},
	}
}
