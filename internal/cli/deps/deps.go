// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/ghaoutput"
	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/internal/adapters/gitlaboutput"
	"github.com/diggsweden/reusable-ci/internal/adapters/jsonsink"
	"github.com/diggsweden/reusable-ci/internal/adapters/local"
	"github.com/diggsweden/reusable-ci/internal/adapters/manifest"
	"github.com/diggsweden/reusable-ci/internal/adapters/stepsummary"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/platform"
)

// Deps is what use cases receive. Provider is the always-available
// minimum (Name + ResolveContext); CLI commands that need richer
// platform capabilities (release creation, asset upload, SARIF upload,
// token validation) request them via the typed accessors below, which
// surface a friendly "feature X requires platform Y" error when the
// detected platform does not support the role.
type Deps struct {
	Platform     provider.Platform
	Provider     provider.Provider
	OutputSink   ci.OutputSink
	SummarySink  ci.SummarySink
	ManifestSink ci.ManifestSink
}

// RequireTokenValidator returns the TokenValidator role when the
// active provider implements it. github and gitlab do; local does
// not — invoking a CLI command that needs this on local surfaces a
// typed error here rather than a runtime ErrUnsupported deep in the
// use case.
func (d *Deps) RequireTokenValidator() (provider.TokenValidator, error) {
	v, ok := d.Provider.(provider.TokenValidator)
	if !ok {
		return nil, unsupportedRoleError(d.Platform, "token validation")
	}

	return v, nil
}

// RequireReleaseCreator returns the ReleaseCreator role when the
// active provider implements it. github and gitlab do; local does not.
func (d *Deps) RequireReleaseCreator() (provider.ReleaseCreator, error) {
	c, ok := d.Provider.(provider.ReleaseCreator)
	if !ok {
		return nil, unsupportedRoleError(d.Platform, "release creation")
	}

	return c, nil
}

// RequireReleaseAssetUploader returns the ReleaseAssetUploader role
// when the active provider implements it. Today only github does.
func (d *Deps) RequireReleaseAssetUploader() (provider.ReleaseAssetUploader, error) {
	u, ok := d.Provider.(provider.ReleaseAssetUploader)
	if !ok {
		return nil, unsupportedRoleError(d.Platform, "release asset upload")
	}

	return u, nil
}

// RequireSARIFUploader returns the SARIFUploader role when the active
// provider implements it. Today only github does — gitlab consumes
// the JSON SAST report directly from trivy/opengrep.
func (d *Deps) RequireSARIFUploader() (provider.SARIFUploader, error) {
	u, ok := d.Provider.(provider.SARIFUploader)
	if !ok {
		return nil, unsupportedRoleError(d.Platform, "SARIF upload to Code Scanning")
	}

	return u, nil
}

// RepoMetadataFetcher returns the RepoMetadataFetcher role. All three
// adapters implement it (local returns empty), so this never errors —
// the helper exists purely so callers depend on the narrow role
// interface rather than the concrete provider.
func (d *Deps) RepoMetadataFetcher() provider.RepoMetadataFetcher {
	return d.Provider.(provider.RepoMetadataFetcher) //nolint:forcetypeassert // every wired Provider implements it; compile-time enforced by adapter conformance vars
}

func unsupportedRoleError(p provider.Platform, capability string) error {
	return fmt.Errorf("%s is not supported on platform %q: %w", capability, p, errs.ErrUnsupported)
}

// Build produces a *Deps wired for the detected platform. Most CLI actions
// should use With so sink close errors are propagated consistently.
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

// WithFormat builds Deps, runs fn, and closes any sink resources.
// When format resolves to JSON the OutputSink is replaced by a
// jsonsink writing to the supplied stdout writer (defaults to
// os.Stdout when nil); other formats keep the platform-detected sink
// so workflows still write to $GITHUB_OUTPUT.
//
// Close errors are returned when fn succeeds so CI output flush
// failures cannot be hidden.
//
// CLI Actions use FromCmd, which reads the format from the root flag
// and forwards here. WithFormat is also the primitive for non-CLI
// callers (tests, library use) that already know the format.
func WithFormat(ctx context.Context, format output.Format, stdout io.Writer, fn func(*Deps) error) (err error) {
	d, err := Build(ctx) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		return err
	}

	if format == output.FormatJSON {
		// Replace the platform sink with one that emits the structured
		// outputs as a JSON object on the chosen writer. The platform
		// sink is closed eagerly here so we never leave its handle
		// dangling — the json sink takes over the contract.
		if d.OutputSink != nil {
			_ = d.OutputSink.Close(ctx)
		}

		if stdout == nil {
			stdout = os.Stdout
		}

		d.OutputSink = jsonsink.New(stdout)
	}

	defer func() {
		if closeErr := d.Close(ctx); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	return fn(d)
}

// FromCmd resolves the root format flag from cmd and forwards to
// WithFormat. The standard entry point for CLI Actions.
func FromCmd(ctx context.Context, cmd *cli.Command, fn func(*Deps) error) error {
	format, err := OutputFormat(cmd)
	if err != nil {
		return err
	}

	return WithFormat(ctx, format, os.Stdout, fn)
}

func buildInternal(_ context.Context) (*Deps, error) {
	p := platform.Detect() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	d := &Deps{Platform: p} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	switch p {
	case provider.PlatformGitHub:
		d.Provider = github.New()
		d.OutputSink = ghaoutput.NewFromEnv()
		d.SummarySink = stepsummary.New(os.Getenv("GITHUB_STEP_SUMMARY"))
	case provider.PlatformGitLab:
		d.Provider = gitlab.New()
		d.OutputSink = gitlaboutput.NewFromEnv()
		d.SummarySink = stepsummary.New(os.Getenv("CI_SUMMARY_FILE"))
	case provider.PlatformLocal:
		d.Provider = local.New()
		d.OutputSink = ghaoutput.New(os.DevNull)
		d.SummarySink = stepsummary.New("")
	default:
		return nil, fmt.Errorf("unsupported platform: %q: %w", p, errs.ErrValidation)
	}

	// ManifestSink: writes <stage>-result.json under $CI_RESULTS_DIR
	// (default ".ci-results"). Cross-job consumers read from here.
	d.ManifestSink = manifest.NewFromEnv()

	return d, nil
}

// Close releases any resources held by the wired adapters. Today only
// OutputSink holds a file handle; new adapters that need cleanup register
// themselves here so Actions don't grow knowledge of which sinks happen to be
// closeable.
func (d *Deps) Close(ctx context.Context) error {
	if d == nil || d.OutputSink == nil {
		return nil
	}

	return d.OutputSink.Close(ctx)
}

// Annotator builds the CLI-side output.Annotator that writes to
// stderr in the format selected by the root --format flag (or --json
// shortcut). Hides the cmd.Root()/flag-name/platform-detect chain
// that would otherwise be replicated at every Action.
func Annotator(cmd *cli.Command) output.Annotator {
	return output.AnnotatorFromFlag(os.Stderr, rawFormat(cmd), platform.Detect())
}

// OutputFormat returns the resolved output format selected by the root
// --format flag (or --json shortcut) — FormatAuto is resolved against
// the active CI platform. Hides the cmd.Root()/flag-name/platform-detect
// chain that would otherwise be replicated at every Action.
//
// Empty input (e.g. tests that build a subgroup directly and bypass
// the root flag default) is treated as FormatAuto, mirroring the
// root flag's Value default.
func OutputFormat(cmd *cli.Command) (output.Format, error) {
	raw := rawFormat(cmd)
	if raw == "" {
		raw = string(output.FormatAuto)
	}

	return output.ParseAndResolve(raw, platform.Detect())
}

// rawFormat reads the root format selection, applying the --json
// shortcut. Kept in deps to avoid leaking the precedence rule into
// every Action; the equivalent logic in internal/cli/root.go's Before
// hook and this helper must move together.
func rawFormat(cmd *cli.Command) string {
	if cmd.Root().Bool("json") {
		return string(output.FormatJSON)
	}

	return cmd.Root().String("format")
}
