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

	"github.com/diggsweden/reusable-ci/internal/adapters/forgejo"
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

// requireRole is the single gate behind every Deps.RequireX accessor: it
// returns the active provider's R role view, or a typed "capability is
// not supported on platform Y" error when the provider does not implement
// R. Each accessor below just names its role type and human-readable
// capability and delegates here, so the assert-or-explain logic lives in
// exactly one place.
func requireRole[R any](d *Deps, capability string) (R, error) {
	role, ok := d.Provider.(R)
	if !ok {
		var zero R

		return zero, unsupportedRoleError(d.Platform, capability)
	}

	return role, nil
}

// RequireTokenValidator returns the TokenValidator role when the
// active provider implements it. github and gitlab do; local does
// not — invoking a CLI command that needs this on local surfaces a
// typed error here rather than a runtime ErrUnsupported deep in the
// use case.
func (d *Deps) RequireTokenValidator() (provider.TokenValidator, error) {
	return requireRole[provider.TokenValidator](d, "token validation")
}

// RequireReleaseCreator returns the ReleaseCreator role when the
// active provider implements it. github and gitlab do; local does not.
func (d *Deps) RequireReleaseCreator() (provider.ReleaseCreator, error) {
	return requireRole[provider.ReleaseCreator](d, "release creation")
}

// RequireReleaseAssetUploader returns the ReleaseAssetUploader role
// when the active provider implements it. Today only github does.
func (d *Deps) RequireReleaseAssetUploader() (provider.ReleaseAssetUploader, error) {
	return requireRole[provider.ReleaseAssetUploader](d, "release asset upload")
}

// RequireRunArtifactUploader returns the RunArtifactUploader role when
// the active provider implements it.
func (d *Deps) RequireRunArtifactUploader() (provider.RunArtifactUploader, error) {
	return requireRole[provider.RunArtifactUploader](d, "run-artifact upload")
}

// RequireRunArtifactDownloader returns the RunArtifactDownloader role
// when the active provider implements it.
func (d *Deps) RequireRunArtifactDownloader() (provider.RunArtifactDownloader, error) {
	return requireRole[provider.RunArtifactDownloader](d, "run-artifact download")
}

// RequireSARIFUploader returns the SARIFUploader role when the active
// provider implements it. Today only github does — gitlab consumes
// the JSON SAST report directly from trivy/opengrep.
func (d *Deps) RequireSARIFUploader() (provider.SARIFUploader, error) {
	return requireRole[provider.SARIFUploader](d, "SARIF upload to Code Scanning")
}

// RequireTagDeleter returns the TagDeleter role when the active provider
// implements it. Forge-specific: deleting a container tag goes through
// each forge's package API (forgejo does; github/gitlab/local not yet),
// because a generic OCI delete is unsafe on the shared-digest promotion
// model. Callers (e.g. `container ledger cleanup`) gate here.
func (d *Deps) RequireTagDeleter() (provider.TagDeleter, error) {
	return requireRole[provider.TagDeleter](d, "container tag deletion")
}

// RequireProvenanceProfiler returns the ProvenanceProfiler role when the
// active provider implements it. The GHA-compatible forges (github,
// forgejo) do; gitlab/local do not — those gate here with a typed
// "unsupported" error rather than emitting a dishonest predicate.
func (d *Deps) RequireProvenanceProfiler() (provider.ProvenanceProfiler, error) {
	return requireRole[provider.ProvenanceProfiler](d, "SLSA provenance generation")
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
	// Two orthogonal axes: the forge API (which server to call) and the
	// runner conventions (which output dialect to emit). Forgejo runs on
	// a GitHub-compatible runner but speaks a Gitea forge API, so the two
	// switches below deliberately resolve independently.
	forge := platform.Detect()
	runner := platform.DetectRunner()
	d := &Deps{Platform: forge} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	if err := wireProvider(d, forge); err != nil {
		return nil, err
	}

	if err := wireSinks(d, runner); err != nil {
		return nil, err
	}

	// ManifestSink: writes <stage>-result.json under $CI_RESULTS_DIR
	// (default ".ci-results"). Cross-job consumers read from here.
	d.ManifestSink = manifest.NewFromEnv()

	return d, nil
}

// wireProvider selects the forge-API adapter for the detected platform.
func wireProvider(d *Deps, forge provider.Platform) error { //nolint:varnamelen // idiomatic short name (matches buildInternal's receiver-style d).
	p, err := providerFor(forge)
	if err != nil {
		return err
	}

	d.Provider = p

	return nil
}

// providerFor returns the forge-API adapter for a platform. This is the
// single provider-construction switch in the codebase; app/domain code
// asks the returned provider (via the Describer / Capabilities / role
// interfaces) rather than branching on the platform enum itself.
func providerFor(forge provider.Platform) (provider.Provider, error) {
	switch forge {
	case provider.PlatformGitHub:
		return github.New(), nil
	case provider.PlatformGitLab:
		return gitlab.New(), nil
	case provider.PlatformForgejo:
		return forgejo.New(), nil
	case provider.PlatformLocal:
		return local.New(), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %q: %w", forge, errs.ErrValidation)
	}
}

// DescriberForDetected returns the forge self-description for the
// auto-detected platform. CLI helpers that need provider metadata (e.g.
// the default OIDC issuer) outside a fully-built *Deps use this so they
// reuse the single providerFor factory instead of re-deriving the
// platform→provider mapping. Every adapter implements Describer, so the
// type assertion always succeeds; the local fallback is defensive.
func DescriberForDetected() provider.Describer {
	p, err := providerFor(platform.Detect())
	if err != nil {
		return local.New()
	}

	if d, ok := p.(provider.Describer); ok {
		return d
	}

	return local.New()
}

// CapabilitiesForDetected returns the optional-feature set of the
// auto-detected forge. Reporting surfaces (doctor) use it to show which
// behaviours degrade on the active forge. Every adapter implements
// CapabilityReporter; the empty set is the defensive fallback.
func CapabilitiesForDetected() provider.Capabilities {
	p, err := providerFor(platform.Detect())
	if err != nil {
		return provider.Capabilities{}
	}

	if c, ok := p.(provider.CapabilityReporter); ok {
		return c.Capabilities()
	}

	return provider.Capabilities{}
}

// wireSinks selects the output + step-summary sinks for the detected
// runner conventions. RunnerGHA serves both GitHub and Forgejo: a
// Forgejo runner exports the same $GITHUB_OUTPUT / $GITHUB_STEP_SUMMARY
// files the GitHub sinks write to.
func wireSinks(d *Deps, runner provider.RunnerKind) error { //nolint:varnamelen // idiomatic short name (matches buildInternal's receiver-style d).
	switch runner {
	case provider.RunnerGHA:
		d.OutputSink = ghaoutput.NewFromEnv()
		d.SummarySink = stepsummary.New(os.Getenv("GITHUB_STEP_SUMMARY"))
	case provider.RunnerGitLab:
		d.OutputSink = gitlaboutput.NewFromEnv()
		d.SummarySink = stepsummary.New(os.Getenv("CI_SUMMARY_FILE"))
	case provider.RunnerLocal:
		d.OutputSink = ghaoutput.New(os.DevNull)
		d.SummarySink = stepsummary.New("")
	default:
		return fmt.Errorf("unsupported runner: %q: %w", runner, errs.ErrValidation)
	}

	return nil
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
	return output.AnnotatorFromFlag(os.Stderr, rawFormat(cmd), platform.DetectRunner())
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

	return output.ParseAndResolve(raw, platform.DetectRunner())
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
