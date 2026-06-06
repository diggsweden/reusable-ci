// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cli assembles the top-level urfave/cli v3 command tree and
// related CLI-facing helpers.
//
// The package wires global flags, subgroups, and docs rendering. It does not
// implement any business logic — that lives under internal/app/.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	cmdbuild "github.com/diggsweden/reusable-ci/internal/cli/commands/build"
	cmdconfig "github.com/diggsweden/reusable-ci/internal/cli/commands/config"
	cmdcontainer "github.com/diggsweden/reusable-ci/internal/cli/commands/container"
	cmddoctor "github.com/diggsweden/reusable-ci/internal/cli/commands/doctor"
	cmdplan "github.com/diggsweden/reusable-ci/internal/cli/commands/plan"
	cmdplatform "github.com/diggsweden/reusable-ci/internal/cli/commands/platform"
	cmdpublish "github.com/diggsweden/reusable-ci/internal/cli/commands/publish"
	cmdrelease "github.com/diggsweden/reusable-ci/internal/cli/commands/release"
	cmdreport "github.com/diggsweden/reusable-ci/internal/cli/commands/report"
	cmdsbom "github.com/diggsweden/reusable-ci/internal/cli/commands/sbom"
	cmdsecurity "github.com/diggsweden/reusable-ci/internal/cli/commands/security"
	cmdvalidate "github.com/diggsweden/reusable-ci/internal/cli/commands/validate"
	cmdversion "github.com/diggsweden/reusable-ci/internal/cli/commands/version"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// BuildInfo carries ldflags-injected version metadata from main.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// New builds the root *cli.Command. Subgroups are added here as each
// migration phase lands them.
func New(b BuildInfo) *cli.Command {
	versionString := fmt.Sprintf("%s (commit %s, built %s)", b.Version, b.Commit, b.Date)

	root := newRoot(b, versionString)
	// Apply OnUsageError + CommandNotFound to every node so subcommand
	// flag-parse errors get the same concise treatment as root-level
	// ones. urfave/cli does not inherit these hooks automatically.
	applyDiagnosticHooks(root)

	return root
}

// applyDiagnosticHooks walks the command tree and sets the
// project-wide OnUsageError hook on every node so the help-dump
// suppression works at any depth.
//
// We deliberately do NOT set CommandNotFound: in urfave/cli v3 a
// non-nil CommandNotFound replaces the default missing-subcommand
// error path entirely, including the error return — so even a noop
// hook causes unknown-subcommand invocations to exit 0 silently.
// The "No help topic for X" message urfave produces by default is
// rewritten to "unknown subcommand X" in cli.ClassifyError instead.
func applyDiagnosticHooks(cmd *cli.Command) {
	cmd.OnUsageError = onUsageError

	for _, child := range cmd.Commands {
		applyDiagnosticHooks(child)
	}
}

func newRoot(_ BuildInfo, versionString string) *cli.Command { //nolint:unparam // BuildInfo retained for symmetry with New(b); future fields will live on this struct.
	return &cli.Command{
		Name:    "reusable-ci",
		Usage:   "shared CI/CD logic for diggsweden/reusable-ci workflows",
		Version: versionString,
		// Description appears under the NAME block in help — the
		// place clig.dev recommends for issue tracker + docs links
		// and a couple of canonical examples. With ~80 subcommands,
		// the root help points at the most common entry points and
		// defers the rest to per-subcommand --help.
		Description: `Docs:    https://github.com/diggsweden/reusable-ci/tree/main/docs
Issues:  https://github.com/diggsweden/reusable-ci/issues

EXAMPLES:
   # Validate artifacts.yml against the v3 schema
   reusable-ci config validate --file artifacts.yml

   # Bump the version-of-record for an NPM project
   reusable-ci version bump --project-type=npm --version=1.2.3

   # Generate every requested CISA SBOM layer locally
   reusable-ci sbom generate all --project-type=npm

Pass --help on any subcommand for the full flag list.`,
		// Suggest enables urfave/cli's built-in Levenshtein-based "did you
		// mean X?" suggestion on unknown subcommands.
		Suggest: true,
		// ExitErrHandler is a no-op so urfave/cli does NOT call os.Exit
		// itself via HandleExitCoder when Action returns an ExitCoder
		// error (e.g. urfave/cli's own "Required flag" / "No help topic"
		// classes). The error flows back to cmd/reusable-ci/main.go where
		// it is classified into our sysexits.h-aligned ExitCode ladder.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		// OnUsageError replaces urfave/cli's default behaviour of dumping
		// the full help text to stderr on a flag-parse failure. The
		// concise "Error: ..." line printed by main.go is enough; the
		// user already knows where help lives.
		OnUsageError: onUsageError,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				// Raises slog's threshold to error. The structured
				// command output that subcommands print directly
				// (`Binary/Module/Version` blocks, build progress
				// lines, …) is NOT affected — that text is the
				// command's result, not log noise. Pipe stderr to
				// /dev/null if you only want machine-readable JSON
				// on stdout.
				Usage:   "raise the log threshold to error (does not suppress command output)",
				Sources: cli.EnvVars("REUSABLE_CI_QUIET"),
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "logging level: debug, info, warn, error (DEBUG=1/true/yes/on is a shortcut for debug)",
				Value:   "info", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources: cli.EnvVars("REUSABLE_CI_LOG"),
			},
			&cli.StringFlag{
				// --format selects the output format envelope
				// (auto/text/json/github/gitlab). It is intentionally
				// long-form only: clig.dev reserves -o/--output for
				// an output FILE, and subcommands like `publish npm
				// write-npmrc --output -` use --output in that
				// conventional sense.
				Name:    "format",
				Usage:   "output format: auto, text, json, github, gitlab",
				Value:   string(output.FormatAuto),
				Sources: cli.EnvVars("REUSABLE_CI_FORMAT"),
			},
			&cli.BoolFlag{
				// --json is the clig.dev-standard shortcut for
				// "give me machine-readable JSON". It is sugar over
				// --format=json and overrides --format if both are
				// set, mirroring the convention used by gh, kubectl,
				// and similar tools.
				Name:    "json",
				Usage:   "shortcut for --format=json (overrides --format)",
				Sources: cli.EnvVars("REUSABLE_CI_JSON"),
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if err := configureLogger(cmd); err != nil {
				return ctx, err
			}
			// --json wins over --format. The same precedence rule
			// lives in deps.rawFormat for non-root call sites that
			// must reach through cmd.Root() — keep both in sync.
			raw := cmd.String("format")
			if cmd.Bool("json") {
				raw = string(output.FormatJSON)
			}

			if _, err := output.Parse(raw); err != nil {
				return ctx, fmt.Errorf("%w: %w", err, errs.ErrUsage)
			}

			return ctx, nil
		},
		Commands: []*cli.Command{
			cmdbuild.New(),
			cmdplatform.New(),
			cmdconfig.New(),
			cmdcontainer.New(),
			cmddoctor.New(),
			cmdplan.New(),
			cmdpublish.New(),
			cmdrelease.New(),
			cmdsbom.New(),
			cmdsecurity.New(),
			cmdreport.New(),
			cmdvalidate.New(),
			cmdversion.New(),
		},
	}
}

// configureLogger wires slog to stderr at the requested level.
// Quiet mode raises the threshold to error.
func configureLogger(cmd *cli.Command) error {
	level := resolveLogLevel(
		cmd.String("log-level"),
		cmd.IsSet("log-level"),
		cmd.Bool("quiet"),
		isTruthyEnv("DEBUG"),
	)

	var slogLevel slog.Level

	switch strings.ToLower(level) {
	case "debug": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn", "warning": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		slogLevel = slog.LevelWarn
	case "error": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		slogLevel = slog.LevelError
	default:
		return fmt.Errorf("invalid log-level %q (want debug/info/warn/error): %w", level, errs.ErrUsage)
	}

	// clig.dev §Output: "Don't treat stderr like a log file, at least
	// not by default." At info/warn/error we render one undecorated
	// line per record via plainHandler; at debug we keep the structured
	// TextHandler so operators piping stderr to a file still get
	// timestamps and attrs.
	var handler slog.Handler
	if slogLevel == slog.LevelDebug {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	} else {
		handler = newPlainHandler(os.Stderr, slogLevel)
	}

	slog.SetDefault(slog.New(handler))

	return nil
}

// isTruthyEnv reports whether the named env var holds a conventional
// "truthy" value (1, true, yes, on — case-insensitive). The empty
// string and any other value is falsy. Matches the convention used by
// most CLI tooling for $DEBUG / $CI / $NO_COLOR-style flags.
func isTruthyEnv(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	}

	return false
}

// resolveLogLevel returns the effective slog level string given the
// flag value, whether the flag was explicitly set (CLI or env source),
// the quiet boolean, and whether $DEBUG is truthy.
//
// Precedence (highest to lowest):
//
//  1. --quiet → "error" (always wins; matches existing UX)
//  2. --log-level on argv, or $REUSABLE_CI_LOG (urfave's IsSet covers both)
//  3. $DEBUG truthy → "debug" (clig.dev general-purpose env-var convention)
//  4. the "info" default
//
// Pure function for testability — no global slog state, no env reads
// beyond what the caller passes in.
func resolveLogLevel(flagLevel string, flagSet, quiet, debugTruthy bool) string {
	if quiet {
		return "error"
	}

	if !flagSet && flagLevel == "info" && debugTruthy {
		return "debug"
	}

	return flagLevel
}

// onUsageError replaces urfave/cli's default behaviour of dumping the
// full help text to stderr on a flag-parse failure. We print the
// error itself + a one-line pointer to --help, then return the error
// so main.go can also set the exit code. The IsAlreadyPrintedByCLI
// check there suppresses the duplicate "Error: ..." line.
//
// helpPath is computed from the command hierarchy so subcommand
// failures point at the right help.
func onUsageError(_ context.Context, cmd *cli.Command, err error, _ bool) error {
	_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
	_, _ = fmt.Fprintf(os.Stderr, "Run '%s --help' for available flags.\n", helpPath(cmd))

	return err
}

// helpPath returns the space-separated command path the user should
// pass to --help. urfave/cli's FullName() already produces this
// (parent commands joined with space); we just guard against a nil
// receiver so the helpers stay defensive.
func helpPath(cmd *cli.Command) string {
	if cmd == nil {
		return "reusable-ci"
	}

	return cmd.FullName()
}
