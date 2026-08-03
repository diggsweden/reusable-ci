// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// PrerequisitesDeps bundles the adapter surfaces the orchestrator needs.
// Each is independent so the caller can stub one without touching the
// rest. Keeping these as interfaces (vs concrete adapters) lets the test
// suite drive the whole orchestrator without spawning git or network
// clients.
//
// Provider is the TokenValidator role — github and gitlab both satisfy
// it; local does not (the CLI gates on platform before constructing
// PrerequisitesDeps).
type PrerequisitesDeps struct {
	GitRepo  gitOps
	Provider provider.TokenValidator
	Cargo    CargoTool
}

// PrerequisitesInput carries every flag and secret used by the
// concurrent validators. Empty secrets are treated as "not configured":
// the corresponding validator either skips or fails per its own rules.
type PrerequisitesInput struct {
	// Workflow context — non-secret.
	Tag        string // github.ref_name when ref_type == tag
	RefType    string // github.ref_type
	Ref        string // github.ref
	Branch     string // target branch (inputs.branch)
	Repository string // github.repository

	// Policy flags from the release plan.
	//
	// RequireAllowlistedSigner gates the tag-signature check on the
	// committed allowlist files (.reusable-ci/allowed_signers for SSH,
	// .reusable-ci/allowed_gpg_keys.asc for GPG). True when any artifact's
	// require-authorization is set OR the workflow input explicitly enables
	// it. When true, a missing/empty allowlist or an unverifiable signature
	// fails closed (ErrPermissionDenied → exit 77).
	RequireAllowlistedSigner bool
	SignArtifacts            bool
	HasMavenCentralTarget    bool
	HasCargoTarget           bool
	HasJVMTarget             bool // any Maven, Gradle, or Gradle-Android artifact in the plan

	// Secrets — passed in by the orchestrator after reading from env so
	// nothing winds up in argv. Empty values surface a validator-specific
	// error from the relevant check (e.g. token validation fails on an
	// empty token).
	ReleaseGPGPublicKey  string
	ReleaseToken         string
	MavenCentralUsername string
	MavenCentralPassword string

	// Cargo prerequisite plan — the same publish-stage plan the
	// existing `validate cargo` reads. Pass-through; the validator
	// parses it.
	PublishStagePlanJSON string

	// Config plan — drives JVM reproducibility check (it iterates
	// Maven/Gradle artifacts to read each manifest). Same JSON that
	// the orchestrator passes to every other stage.
	ConfigPlanJSON string
}

// PrerequisitesResult records the outcome of every applicable validator
// in declaration order. Skipped validators (their gating flag was off)
// carry SkipReason; passed validators carry a one-line note; failed
// validators carry the captured error.
type PrerequisitesResult struct {
	Checks []ValidatorOutcome
}

// HasFailures reports whether any validator returned an error.
func (r PrerequisitesResult) HasFailures() bool {
	for _, c := range r.Checks {
		if c.Err != nil {
			return true
		}
	}

	return false
}

// ValidatorOutcome is one row in the prerequisites report.
type ValidatorOutcome struct {
	Name       string // stable identifier (e.g. "tag-format")
	Skipped    bool
	SkipReason string
	Err        error // nil on success
	// Output is the validator's per-check w. Captured per-validator
	// so concurrent execution doesn't interleave lines.
	Output string
}

// CheckOrder is the canonical declaration order used by the rendered
// report. Independent validators run concurrently regardless of the
// position in this list.
//
//nolint:gochecknoglobals // ordered enumeration of check names.
var checkOrder = []string{
	"ref-type",
	"tag-format",
	"tag-uniqueness",
	"tag-commit",
	"tag-signature",
	"gpg-public-key",
	"release-token",
	"bot-permissions",
	"maven-central",
	"cargo",
	"jvm-reproducibility",
}

// Prerequisites runs every applicable validator concurrently with
// errgroup, captures per-validator output to avoid interleaving, and
// returns a typed result. A non-nil error is returned when at least one
// validator failed; the result is always populated regardless.
//
// Validators are independent: the git tag fetch happens before this
// call (caller's responsibility); inside Prerequisites every validator
// races against the others.
//
// Wall-clock target: replaces ~10 sequential workflow steps (each
// paying ~3s container/process startup) with one Go invocation running
// the underlying checks in parallel.
//
//nolint:cyclop // validates one provider/env/secret/bin/file invariant per branch.
func Prerequisites(
	ctx context.Context,
	deps PrerequisitesDeps,
	w io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in PrerequisitesInput,
) (PrerequisitesResult, error) {
	if deps.GitRepo == nil || deps.Provider == nil {
		return PrerequisitesResult{}, fmt.Errorf("Prerequisites requires GitRepo and Provider deps: %w", errs.ErrUsage)
	}

	checks := newCheckRegistry()
	g, gctx := errgroup.WithContext(ctx) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	// Tag-related validators only apply when triggered by a tag.
	tagTrigger := strings.EqualFold(in.RefType, "tag")

	// Ref-type itself is always validated — it's how we know whether
	// the tag suite should run.
	checks.run(gctx, g, "ref-type", func(_ context.Context, out *bytes.Buffer) error {
		return RefType(out, RefTypeInput{RefType: provider.RefType(in.RefType), RefName: in.Tag, Ref: in.Ref})
	})

	if tagTrigger {
		// The human pushes a signed `release-request/vX.Y.Z` ref: that tag is
		// the object that exists and carries the signature, so signature /
		// uniqueness / commit-reachability all run against it (in.Tag). Only
		// the *format* check validates the derived final tag (vX.Y.Z) — the
		// release tag the bot will create.
		formatTag := in.Tag
		if final, ok := domainversion.ReleaseRequestVersion(in.Tag); ok {
			formatTag = final
		}

		checks.run(gctx, g, "tag-format", func(_ context.Context, out *bytes.Buffer) error {
			return TagFormat(out, TagFormatInput{Tag: formatTag})
		})
		checks.run(gctx, g, "tag-uniqueness", func(c context.Context, out *bytes.Buffer) error {
			return TagUniqueness(c, deps.GitRepo, out, TagUniquenessInput{Tag: in.Tag})
		})
		checks.run(gctx, g, "tag-commit", func(c context.Context, out *bytes.Buffer) error {
			return TagCommit(c, deps.GitRepo, out, TagCommitInput{Tag: in.Tag, Branch: in.Branch})
		})
		checks.run(gctx, g, "tag-signature", func(c context.Context, out *bytes.Buffer) error {
			return TagSignature(c, deps.GitRepo, out, annot, TagSignatureInput{
				Tag:                      in.Tag,
				Repository:               in.Repository,
				ReleaseGPGPublicKey:      []byte(in.ReleaseGPGPublicKey),
				RequireAllowlistedSigner: in.RequireAllowlistedSigner,
			})
		})
	} else {
		checks.skip("tag-format", "non-tag ref")
		checks.skip("tag-uniqueness", "non-tag ref")
		checks.skip("tag-commit", "non-tag ref")
		checks.skip("tag-signature", "non-tag ref")
	}

	if in.SignArtifacts {
		checks.run(gctx, g, "gpg-public-key", func(_ context.Context, out *bytes.Buffer) error {
			return GPGPublicKey(out, in.ReleaseGPGPublicKey)
		})
	} else {
		checks.skip("gpg-public-key", "sign-artifacts disabled")
	}

	// Release-token + bot-permissions are network probes; they race.
	checks.run(gctx, g, "release-token", func(c context.Context, out *bytes.Buffer) error {
		return Token(c, deps.Provider, out, TokenInput{Token: in.ReleaseToken, Repository: in.Repository})
	})
	checks.run(gctx, g, "bot-permissions", func(c context.Context, out *bytes.Buffer) error {
		return BotPermissions(c, deps.Provider, out, BotPermissionsInput{Repository: in.Repository})
	})

	if in.HasMavenCentralTarget {
		checks.run(gctx, g, "maven-central", func(_ context.Context, out *bytes.Buffer) error {
			return MavenCentralCredentials(out, out, annot, MavenCentralCredentialsInput{
				Username: in.MavenCentralUsername,
				Password: in.MavenCentralPassword,
			})
		})
	} else {
		checks.skip("maven-central", "no maven-central target")
	}

	if in.HasCargoTarget {
		checks.run(gctx, g, "cargo", func(c context.Context, out *bytes.Buffer) error {
			return CargoPrerequisites(c, deps.Cargo, out, annot, CargoPrerequisitesInput{
				ConfigPlanJSON:       in.ConfigPlanJSON,
				PublishStagePlanJSON: in.PublishStagePlanJSON,
			})
		})
	} else {
		checks.skip("cargo", "no cargo target")
	}

	if in.HasJVMTarget {
		checks.run(gctx, g, "jvm-reproducibility", func(c context.Context, out *bytes.Buffer) error {
			return JVMReproducibility(c, out, annot, JVMReproducibilityInput{
				ConfigPlanJSON: in.ConfigPlanJSON,
			})
		})
	} else {
		checks.skip("jvm-reproducibility", "no maven/gradle target")
	}

	// errgroup.Wait returns the first non-nil error; we want every
	// validator to run regardless. We catch the first to honour ctx
	// cancellation semantics but rely on checks.outcomes for the full
	// per-validator picture.
	_ = g.Wait()

	result := checks.collect()
	for _, c := range result.Checks {
		if c.Output != "" {
			_, _ = fmt.Fprint(w, c.Output)

			if !strings.HasSuffix(c.Output, "\n") {
				_, _ = fmt.Fprintln(w)
			}
		}
	}

	if result.HasFailures() {
		var failed []string

		for _, c := range result.Checks {
			if c.Err != nil {
				failed = append(failed, c.Name)
			}
		}

		return result, fmt.Errorf("prerequisites failed: %s: %w", strings.Join(failed, ", "), errs.ErrValidation)
	}

	return result, nil
}

// checkRegistry collects the outcome of each validator. Concurrent
// errgroup goroutines write into it; the registry guards access with a
// mutex and renders the final ordered output.
type checkRegistry struct {
	mu       sync.Mutex
	outcomes map[string]ValidatorOutcome
}

func newCheckRegistry() *checkRegistry {
	return &checkRegistry{outcomes: make(map[string]ValidatorOutcome, len(checkOrder))}
}

// run schedules fn under the errgroup, capturing its per-validator
// w into the registry. fn's error is recorded but not propagated
// to the errgroup so other validators keep running.
func (r *checkRegistry) run(ctx context.Context, g *errgroup.Group, name string, fn func(context.Context, *bytes.Buffer) error) {
	g.Go(func() error {
		var buf bytes.Buffer

		err := fn(ctx, &buf)

		r.mu.Lock()
		r.outcomes[name] = ValidatorOutcome{Name: name, Err: err, Output: buf.String()}
		r.mu.Unlock()

		return nil // never abort siblings
	})
}

// skip records a validator that didn't apply.
func (r *checkRegistry) skip(name, reason string) {
	r.mu.Lock()
	r.outcomes[name] = ValidatorOutcome{Name: name, Skipped: true, SkipReason: reason}
	r.mu.Unlock()
}

// collect returns the outcomes in canonical order.
func (r *checkRegistry) collect() PrerequisitesResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := PrerequisitesResult{Checks: make([]ValidatorOutcome, 0, len(r.outcomes))}
	for _, name := range checkOrder {
		if o, ok := r.outcomes[name]; ok {
			out.Checks = append(out.Checks, o)
		}
	}
	// Anything outside the canonical order — defensive, shouldn't happen.
	for name, o := range r.outcomes {
		if !containsName(out.Checks, name) {
			out.Checks = append(out.Checks, o)
		}
	}

	sort.SliceStable(out.Checks, func(i, j int) bool {
		return canonicalIndex(out.Checks[i].Name) < canonicalIndex(out.Checks[j].Name)
	})

	return out
}

func containsName(checks []ValidatorOutcome, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return true
		}
	}

	return false
}

func canonicalIndex(name string) int {
	for i, n := range checkOrder {
		if n == name {
			return i
		}
	}

	return len(checkOrder)
}

// WritePrerequisitesSummary writes a human-readable summary to the
// step-summary sink. Used by the `validate prerequisites` CLI to
// produce the equivalent of the workflow's old per-step UI ticks in
// one consolidated table.
func WritePrerequisitesSummary(ctx context.Context, sink ci.SummarySink, result PrerequisitesResult) error {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	b.WriteString("## Release Prerequisites\n\n")
	b.WriteString("| Check | Status |\n")
	b.WriteString("|-------|--------|\n")

	for _, c := range result.Checks { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		var status string

		switch {
		case c.Skipped:
			status = fmt.Sprintf("− Skipped (%s)", c.SkipReason)
		case c.Err != nil:
			status = "✗ Failed"
		default:
			status = "✓ Passed"
		}

		_, _ = fmt.Fprintf(&b, "| %s | %s |\n", c.Name, status)
	}

	if result.HasFailures() {
		b.WriteString("\n### ✗ One or more prerequisites failed\n\n")

		for _, c := range result.Checks {
			if c.Err != nil {
				_, _ = fmt.Fprintf(&b, "- **%s**: %s\n", c.Name, errorOneLine(c.Err))
			}
		}
	}

	if warnings := collectWarnings(result); len(warnings) > 0 {
		b.WriteString("\n> [!WARNING]\n")

		for _, w := range warnings {
			_, _ = fmt.Fprintf(&b, "> %s\n", w)
		}
	}

	return sink.Append(ctx, b.String())
}

// collectWarnings gathers the ⚠️-prefixed lines that validators write to
// their per-check output (e.g. "no signer allowlist enforced"). They're
// non-fatal but security-relevant, so they get a prominent GitHub
// [!WARNING] callout in the summary panel rather than being lost in the
// log. The matching ::warning:: workflow command (emitted alongside)
// surfaces the same thing in the Annotations pane.
func collectWarnings(result PrerequisitesResult) []string {
	var warnings []string

	for _, c := range result.Checks {
		for _, line := range strings.Split(c.Output, "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "⚠️") {
				warnings = append(warnings, trimmed)
			}
		}
	}

	return warnings
}

// errorOneLine returns the first non-empty line of err. Errors that
// embed multi-line tool output would otherwise blow up the table.
func errorOneLine(err error) string {
	if err == nil {
		return ""
	}

	for _, line := range strings.Split(err.Error(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}

	return ""
}

// ignoreErrgroupResult is unused; reserved for a future when we want
// errgroup cancellation on first failure. Left here as a marker.
var _ = errors.Is
