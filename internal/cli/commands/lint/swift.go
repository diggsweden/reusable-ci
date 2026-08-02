// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lint

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/swiftformat"
	swiftlintad "github.com/diggsweden/reusable-ci/v3/internal/adapters/swiftlint"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/commonflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func swiftCmd() *cli.Command {
	return &cli.Command{
		Name:  "swift",
		Usage: "Swift/macOS lint wrappers (swift-format, swiftlint)",
		Commands: []*cli.Command{
			swiftFormatLintCmd(),
			swiftLintLintCmd(),
		},
	}
}

func swiftFormatLintCmd() *cli.Command {
	return &cli.Command{
		Name:  "format-lint",
		Usage: "enumerate Swift files via `git ls-files`, run `swift-format lint -s`, and write the step-summary block",
		Description: `EXAMPLE:
   reusable-ci build swift format-lint --working-dir .`,
		Flags: []cli.Flag{
			commonflags.WorkingDir("directory the file walk is rooted at"),
			&cli.StringFlag{Name: "file-pattern", Value: "*.swift", Sources: cli.EnvVars("FILE_PATTERN"), Usage: "git pathspec pattern matched against tracked files"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.SwiftFormatLint(ctx, git.New(), swiftformat.New(), d.SummarySink, os.Stderr, os.Stderr, annot, appbuild.SwiftFormatLintInput{
					Dir:         cmd.String("working-dir"),
					FilePattern: cmd.String("file-pattern"),
				})
			})
		},
	}
}

func swiftLintLintCmd() *cli.Command {
	return &cli.Command{
		Name:  "swiftlint",
		Usage: "run `swiftlint lint` with optional --config and write the step-summary block",
		Description: `EXAMPLE:
   reusable-ci build swift swiftlint --config .swiftlint.yml --fail-on-warning`,
		Flags: []cli.Flag{
			commonflags.WorkingDir("directory the linter is rooted at"),
			&cli.StringFlag{Name: "config", Sources: cli.EnvVars("SWIFTLINT_CONFIG_PATH"), Usage: "path to a .swiftlint.yml config file (passed via --config)"},
			&cli.BoolFlag{Name: "fail-on-warning", Sources: cli.EnvVars("FAIL_ON_WARNING"), Usage: "warnings cause a non-zero exit"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.SwiftLintLint(ctx, swiftLintRunner{sl: swiftlintad.New()}, d.SummarySink, os.Stderr, os.Stderr, annot, appbuild.SwiftLintLintInput{
					Dir:           cmd.String("working-dir"),
					ConfigPath:    cmd.String("config"),
					FailOnWarning: cmd.Bool("fail-on-warning"),
				})
			})
		},
	}
}

// swiftLintRunner adapts adapters/swiftlint.Adapter to the app's
// SwiftLintOps interface by converting the named input types across
// the layer boundary (keeps adapter types out of app imports).
type swiftLintRunner struct{ sl *swiftlintad.Adapter }

func (r swiftLintRunner) Lint(ctx context.Context, in appbuild.SwiftLintRunInput) (string, int, error) {
	return r.sl.Lint(ctx, swiftlintad.LintInput{Dir: in.Dir, ConfigPath: in.ConfigPath})
}
