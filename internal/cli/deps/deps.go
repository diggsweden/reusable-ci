// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package deps wires the platform-selected provider and CI sink adapters.
//
// Some CLI subcommands still construct narrow tool adapters directly,
// but provider/output/summary/manifest selection lives here so the
// platform-specific wiring has one home.
//
// Build(ctx) reads the platform via internal/platform.Detect() and
// returns a typed *Deps with adapters wired in.
package deps

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/ghaoutput"
	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/internal/adapters/local"
	"github.com/diggsweden/reusable-ci/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/internal/adapters/stepsummary"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/platform"
)

// Deps is what use cases receive.
type Deps struct {
	Platform     provider.Platform
	Provider     provider.Provider
	OutputSink   ci.OutputSink
	SummarySink  ci.SummarySink
	ManifestSink ci.ManifestSink
}

// Build produces a *Deps wired for the detected platform. Callers
// should `defer d.Close(ctx)` to flush any sinks that hold resources.
//
// Errors are pre-wrapped with "init deps:" so every CLI Action can
// simply `return err` without restating the context — the wrap
// belongs to Build's contract (its job IS to init deps), not to each
// of the 40+ subcommands.
func Build(ctx context.Context) (*Deps, error) {
	d, err := buildInternal(ctx)
	if err != nil {
		return nil, fmt.Errorf("init deps: %w", err)
	}
	return d, nil
}

func buildInternal(_ context.Context) (*Deps, error) {
	p := platform.Detect()
	d := &Deps{Platform: p}

	switch p {
	case provider.PlatformGitHub:
		d.Provider = github.New()
	case provider.PlatformGitLab:
		d.Provider = gitlab.New()
	case provider.PlatformLocal:
		d.Provider = local.New()
	default:
		return nil, fmt.Errorf("unsupported platform: %q", p)
	}

	// OutputSink: same GHA-style heredoc shape on GitHub + Local.
	// GitLab will get a dotenv adapter when use cases start writing
	// outputs from inside the GitLab provider branch.
	d.OutputSink = ghaoutput.NewFromEnv()

	// SummarySink: append-only writer to $GITHUB_STEP_SUMMARY (GHA) or
	// $CI_SUMMARY_FILE. No-op when neither is set — matches the bash.
	d.SummarySink = stepsummary.NewFromEnv()

	// ManifestSink: writes <stage>-result.json under $CI_RESULTS_DIR
	// (default ".ci-results"). Cross-job consumers read from here.
	d.ManifestSink = manifest.NewFromEnv()

	return d, nil
}

// Close releases any resources held by the wired adapters. Callers
// typically `defer d.Close(ctx)` immediately after a successful Build.
// Today only OutputSink holds a file handle; new adapters that need
// cleanup register themselves here so Actions don't grow knowledge of
// which sinks happen to be closeable.
func (d *Deps) Close(ctx context.Context) error {
	if d == nil || d.OutputSink == nil {
		return nil
	}
	return d.OutputSink.Close(ctx)
}

// Annotator builds the CLI-side output.Annotator that writes to
// stderr in the format selected by the root command's --output flag.
// Hides the cmd.Root()/flag-name/platform-detect chain that would
// otherwise be replicated at every Action.
func Annotator(cmd *cli.Command) output.Annotator {
	return output.AnnotatorFromFlag(os.Stderr, cmd.Root().String("output"), platform.Detect())
}
