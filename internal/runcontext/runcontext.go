// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package runcontext names the environment variables that describe the CI
// run a command executes inside — which repository, which ref, which
// commit, which run, where the scratch space is — and fixes the precedence
// between them.
//
// It holds NAMES, not values. Nothing is read at import time and nothing
// here calls os.Getenv; resolution always goes through a caller-supplied
// lookup. That is what keeps the package pure enough to be a leaf utility
// (see internal/archguard) and lets every consumer keep its own test seam.
//
// # Why this is a leaf rather than part of internal/cli/cienv
//
// The layering forbids adapters->cli and app->cli, so a name list living
// under cli/ is unreachable from HALF the hexagon. Those layers coped by
// re-declaring the names inline, and the lists drifted apart — the exact
// failure cienv was created to prevent, reintroduced by cienv's own
// address. The names are not a CLI concept; only their BINDING to
// urfave/cli flags is. So the binding stays in cienv and the names live
// here, where all four layers can reach them.
//
// A concept therefore has exactly ONE definition. cienv turns it into a
// flag's ValueSourceChain; an adapter resolves it against its injected env
// func. Same names, same order, two bindings.
//
// # Precedence convention (first NON-EMPTY name wins)
//
//  1. the project-neutral BARE name the reusable-ci workflows set
//     explicitly (e.g. REPOSITORY, REF_NAME) — the deliberately-computed
//     value the orchestration layer wants the command to use;
//  2. the GitLab/Forgejo-native CI_*/FORGEJO_* name (runner-provided);
//  3. the GitHub-native GITHUB_* name (runner-provided).
//
// Set-but-EMPTY counts as ABSENT throughout: a forge-neutral var blanked by
// a workflow's `${{ env.X || ” }}` expression must fall through to a
// populated fallback rather than shadow it.
//
// Each chain is the UNION of every variant its consumers accept, so existing
// workflows keep working without a per-consumer list of names to drift.
package runcontext

import (
	"net/url"
	"strings"
)

// Name is one environment variable that can carry a concept, together with
// the metadata that decides what its value may be USED for.
//
// The Key alone is not enough, because provenance is not a property of the
// name in the abstract: $GITHUB_REPOSITORY is the runner's own value on
// GitHub AND on Forgejo (act_runner sets the GITHUB_* names as compat
// aliases), while on GitLab the same name could only have been set by a
// workflow. So provenance is a question about (name, runner), answered at
// resolution time — which is exactly what injected holds.
type Name struct {
	// Key is the environment variable name.
	Key string

	// injected reports whether the runner we are executing on sets this
	// name ITSELF. False means the value, if present, was computed by the
	// orchestration layer — convenient, and not a thing to anchor trust on.
	injected func(env func(string) string) bool
}

// orchestrated marks a name the reusable-ci workflows set deliberately
// (REPOSITORY, CI_REPO, and the FORGEJO_REPO / FORGEJO_SERVER aliases). No
// runner injects it, so it is never attested — however trustworthy the
// orchestration layer is, a trust anchor must not depend on a computation
// upstream of it.
func orchestrated(key string) Name {
	return Name{Key: key, injected: func(func(string) string) bool { return false }}
}

// forgejoInjected marks a name a Forgejo/Gitea runner sets about its own run.
func forgejoInjected(key string) Name {
	return Name{Key: key, injected: ForgejoRunner}
}

// valueTrue is the exact value the runners write into their own marker
// variables. Markers are matched exactly rather than through truthy(): a
// runner either sets its marker or it does not, and loosening that would let
// an operator-set "yes" impersonate a runner.
const valueTrue = "true"

// githubInjected marks a name injected by any GitHub-Actions-compatible
// runner — which includes Forgejo's act_runner, since it sets the GITHUB_*
// names as aliases for its own values. That is precisely why $GITHUB_TOKEN is
// Forgejo's credential on Forgejo and GitHub's on GitHub.
func githubInjected(key string) Name {
	return Name{Key: key, injected: func(env func(string) string) bool {
		return env("GITHUB_ACTIONS") == valueTrue
	}}
}

// gitlabInjected marks a name GitLab CI sets about its own run.
func gitlabInjected(key string) Name {
	return Name{Key: key, injected: func(env func(string) string) bool {
		return env("GITLAB_CI") == valueTrue
	}}
}

// Var is an ordered set of environment variable names that all carry the
// same run-context concept. The first NON-EMPTY name wins.
type Var struct {
	// Concept names what the variable means in the CLI's own vocabulary
	// ("repository", "commit", …) — what an error message calls the thing
	// when none of Names is set.
	Concept string

	// Names is the precedence order, most-preferred first. Never empty.
	Names []Name
}

// Keys returns the variable names in precedence order.
func (v Var) Keys() []string {
	out := make([]string, len(v.Names))
	for i, n := range v.Names {
		out[i] = n.Key
	}

	return out
}

// Resolve returns the first non-empty value among v.Names, or "" when the
// run context does not carry this concept.
//
// env is the lookup: os.Getenv at a composition root, or a map-backed fake
// in a test. Injecting it is what keeps this package pure, and what lets an
// adapter resolve the SAME precedence the CLI flags use without importing
// the cli layer.
func (v Var) Resolve(env func(string) string) string {
	for _, name := range v.Names {
		if value := env(name.Key); value != "" {
			return value
		}
	}

	return ""
}

// Attested is a value that came from a name the RUNNER injected.
//
// It is a distinct, opaque type so that a sink which must not be widened by
// anything upstream can demand one. Only ResolveAttested and JoinAttested
// mint a non-empty Attested, so a string obtained from Resolve — which
// deliberately prefers the orchestration layer's computed value — cannot
// reach such a sink by resembling it.
//
// The zero value is empty on purpose: a sink handed one fails closed rather
// than matching something.
type Attested struct{ value string }

// String returns the attested value.
func (a Attested) String() string { return a.value }

// JoinAttested concatenates attested parts with sep, staying attested. It
// exists so composing "<server>/<owner>/<repo>" does not force a caller
// through plain strings and back, which is where the guarantee would be lost.
func JoinAttested(sep string, parts ...Attested) Attested {
	raw := make([]string, len(parts))
	for idx, part := range parts {
		if part.value == "" {
			// One missing part would silently yield a shorter, WIDER
			// value ("https://host//repo" or just "https://host") -- so
			// fail closed rather than anchor on a truncated identity.
			return Attested{}
		}

		raw[idx] = part.value
	}

	return Attested{value: strings.Join(raw, sep)}
}

// ResolveAttested returns the first non-empty value among the names THIS
// RUNNER injects, skipping the ones the orchestration layer computes.
//
// It is a different traversal, not a filter on Resolve's answer, and the
// difference matters. The chains are ordered for describing a run,
// so a computed name comes FIRST: on a Forgejo runner with $REPOSITORY set,
// Resolve returns that, and asking afterwards "was the winner injected?"
// would answer no while $FORGEJO_REPOSITORY sat right behind it, unused. One
// chain, two orderings — "most deliberate wins" for describing, "least
// forgeable wins" for trusting.
func (v Var) ResolveAttested(env func(string) string) (Attested, bool) {
	for _, name := range v.Names {
		if !name.injected(env) {
			continue
		}

		if value := env(name.Key); value != "" {
			return Attested{value: value}, true
		}
	}

	return Attested{}, false
}

// String renders the names as "$A / $B / $C".
//
// It exists for "not set" diagnostics: a message built from this lists
// every name that WOULD have worked, instead of naming whichever single
// one the author happened to have in mind — which is how a caller ends up
// told to set $FORGEJO_REPOSITORY when $CI_REPO was the neutral answer.
func (v Var) String() string {
	return "$" + strings.Join(v.Keys(), " / $")
}

// Run-context concepts. One definition each — every consumer, in every
// layer, references these instead of an inline name list, so precedence and
// fallbacks cannot drift apart again.

// Repository resolves "owner/repo".
//
// As with every chain below that falls back to a $GITHUB_* var: Forgejo's
// native $FORGEJO_* name is preferred ahead of the $GITHUB_* compat alias.
// Forgejo Runner 7.0.0 lets workflows drop the GITHUB_ names entirely, so
// relying on the alias alone is fragile; on GitHub the FORGEJO_* var is
// unset, so the alias still wins there.
//
// FORGEJO_REPO is the forgejo-ci workflows' explicitly-set alias for the
// runner's FORGEJO_REPOSITORY (same value, `forgejo.repository`).
func Repository() Var {
	return Var{
		Concept: "repository",
		Names: []Name{
			orchestrated("REPOSITORY"),
			orchestrated("CI_REPO"),
			forgejoInjected("FORGEJO_REPOSITORY"),
			orchestrated("FORGEJO_REPO"),
			githubInjected("GITHUB_REPOSITORY"),
		},
	}
}

// RepositoryOwner resolves the owning org/user.
func RepositoryOwner() Var {
	return Var{
		Concept: "repository owner",
		Names: []Name{
			orchestrated("REPOSITORY_OWNER"),
			forgejoInjected("FORGEJO_REPOSITORY_OWNER"),
			githubInjected("GITHUB_REPOSITORY_OWNER"),
		},
	}
}

// RefName resolves the short git ref name (branch or tag, no refs/ prefix).
func RefName() Var {
	return Var{
		Concept: "ref name",
		Names: []Name{
			orchestrated("REF_NAME"),
			orchestrated("CI_REF_NAME"),
			forgejoInjected("FORGEJO_REF_NAME"),
			githubInjected("GITHUB_REF_NAME"),
		},
	}
}

// Ref resolves the full git ref (refs/heads/…, refs/tags/…).
func Ref() Var {
	return Var{Concept: "ref", Names: []Name{
		orchestrated("REF"),
		forgejoInjected("FORGEJO_REF"),
		githubInjected("GITHUB_REF"),
	}}
}

// RefType resolves the ref kind ("branch" or "tag").
func RefType() Var {
	return Var{Concept: "ref type", Names: []Name{
		orchestrated("REF_TYPE"),
		forgejoInjected("FORGEJO_REF_TYPE"),
		githubInjected("GITHUB_REF_TYPE"),
	}}
}

// EventName resolves the workflow trigger event in the canonical vocabulary
// (the GitHub-Actions spellings, which GHA and Forgejo emit natively).
//
// GitLab's CI_PIPELINE_SOURCE is deliberately absent: its dialect
// (merge_request_event, web, …) is normalized by the gitlab provider
// adapter's ResolveContext, and commands fall back to that resolved context
// when no env var is set — sourcing the raw var here would bypass the
// normalization.
func EventName() Var {
	return Var{
		Concept: "event name",
		Names: []Name{
			orchestrated("EVENT_NAME"),
			forgejoInjected("FORGEJO_EVENT_NAME"),
			githubInjected("GITHUB_EVENT_NAME"),
		},
	}
}

// Commit resolves the commit SHA. COMMIT_SHA is the bare deliberate name
// the report steps set; it wins over the runner-provided vars but not over
// the CI_COMMIT* pair already in use.
func Commit() Var {
	return Var{
		Concept: "commit",
		Names: []Name{
			orchestrated("CI_COMMIT"),
			gitlabInjected("CI_COMMIT_SHA"),
			orchestrated("COMMIT_SHA"),
			forgejoInjected("FORGEJO_SHA"),
			githubInjected("GITHUB_SHA"),
		},
	}
}

// CheckoutRef resolves the ref `platform checkout` materializes: an explicit
// CHECKOUT_REF (branch, tag, or SHA) takes precedence — letting a workflow
// check out a branch via env — and falls back to the triggering commit so
// the default is unchanged.
func CheckoutRef() Var {
	return Var{
		Concept: "checkout ref",
		Names: []Name{
			orchestrated("CHECKOUT_REF"),
			orchestrated("CI_COMMIT"),
			gitlabInjected("CI_COMMIT_SHA"),
			forgejoInjected("FORGEJO_SHA"),
			githubInjected("GITHUB_SHA"),
		},
	}
}

// RunID resolves the CI run identifier.
func RunID() Var {
	return Var{Concept: "run id", Names: []Name{
		orchestrated("CI_RUN_ID"),
		forgejoInjected("FORGEJO_RUN_ID"),
		githubInjected("GITHUB_RUN_ID"),
	}}
}

// RunURL resolves the human-facing CI run URL.
func RunURL() Var {
	return Var{Concept: "run url", Names: []Name{
		orchestrated("CI_RUN_URL"),
	}}
}

// Actor resolves the triggering user.
//
// The two bindings of this concept had disjoint name lists: the flag chain
// knew only CI_ACTOR while every registry-auth adapter read the runner's
// native FORGEJO_ACTOR/GITHUB_ACTOR, so they shared no name at all and a
// runner-provided actor was invisible to the flag. This is their union.
func Actor() Var {
	return Var{Concept: "actor", Names: []Name{
		orchestrated("CI_ACTOR"),
		forgejoInjected("FORGEJO_ACTOR"),
		githubInjected("GITHUB_ACTOR"),
	}}
}

// ServerURL resolves the forge base URL (e.g. https://forgejo.example.com). It
// builds git-over-HTTPS clone URLs, so it must resolve on every forge:
// CI_SERVER_URL (GitLab-native / neutral) → FORGEJO_SERVER_URL → the
// GitHub-native GITHUB_SERVER_URL the Forgejo runner also sets.
//
// FORGEJO_SERVER is the forgejo-ci workflows' explicitly-set alias for the
// runner's FORGEJO_SERVER_URL (same value, `forgejo.server_url`).
func ServerURL() Var {
	return Var{
		Concept: "server url",
		Names: []Name{
			gitlabInjected("CI_SERVER_URL"),
			forgejoInjected("FORGEJO_SERVER_URL"),
			orchestrated("FORGEJO_SERVER"),
			githubInjected("GITHUB_SERVER_URL"),
		},
	}
}

// TempDir resolves the runner scratch directory.
func TempDir() Var {
	return Var{Concept: "temp dir", Names: []Name{
		orchestrated("CI_TEMP_DIR"),
		orchestrated("RUNNER_TEMP"),
	}}
}

// Workspace resolves the checkout target directory.
func Workspace() Var {
	return Var{
		Concept: "workspace",
		Names: []Name{
			orchestrated("CI_WORKSPACE"),
			forgejoInjected("FORGEJO_WORKSPACE"),
			githubInjected("GITHUB_WORKSPACE"),
		},
	}
}

// tokenVar names the forge API/clone token for the forge this run BELONGS
// to. It is unexported because the NAMES are shared (cienv binds them to a
// flag, All enumerates them) while the VALUE must only ever leave this
// package as a Credential — see Token.
//
// Non-empty semantics matter: an empty GITHUB_TOKEN must not shadow a
// populated FORGEJO_TOKEN, and an empty result means an anonymous
// (public-repo) checkout.
//
// GITEA_TOKEN is the Gitea-native name that Forgejo still accepts.
func tokenVar() Var {
	return Var{Concept: conceptToken, Names: append(forgeNeutralTokenNames(), githubInjected(nameGitHubToken))}
}

// conceptToken labels both token chains: they name the same concept, only
// scoped differently.
const conceptToken = "token"

// nameGitHubToken is the one name whose meaning depends on WHERE we run: a
// Forgejo runner injects the job token under it as a GitHub-compat alias,
// while on GitHub it is GitHub's own. Every chain that includes it has to
// say why.
const nameGitHubToken = "GITHUB_TOKEN" //nolint:gosec // G101: an env var NAME; this package holds names, never values.

// forgeNeutralTokenNames are the names no runner injects on its own: a
// workflow sets them deliberately, so they say which forge they mean.
//
// Returned fresh so callers can append without aliasing a shared backing
// array — and declared once so the two chains below cannot drift.
func forgeNeutralTokenNames() []Name {
	return []Name{
		orchestrated("CI_TOKEN"),
		forgejoInjected("FORGEJO_TOKEN"),
		forgejoInjected("GITEA_TOKEN"),
	}
}

// releaseTokenVar names the write-scoped token used to push the release
// commit and tag. The dedicated RELEASE_TOKEN wins (least-privilege: a
// separate write token, distinct from the read-only clone token), then the
// same chain as tokenVar so the push works on any forge. Empty semantics
// matter here too: an unset RELEASE_TOKEN (a `${{ secrets.* }}` that
// resolved to "") must fall through, not shadow the forge's ambient token.
//
// Built from tokenVar rather than restating it, so the two cannot drift.
func releaseTokenVar() Var {
	return Var{
		Concept: "release token",
		Names:   append([]Name{orchestrated("RELEASE_TOKEN")}, tokenVar().Names...),
	}
}

// Token resolves the forge API/clone token. See CredentialVar for why this
// returns a chain that yields a Credential rather than a string.
func Token() CredentialVar { return CredentialVar{v: tokenVar()} }

// ReleaseToken resolves the write-scoped token for the release commit and tag.
func ReleaseToken() CredentialVar { return CredentialVar{v: releaseTokenVar()} }

// CredentialVar is a token concept. It is a distinct type from Var for one
// reason: Var.Resolve hands back a string, and a bare secret string carries
// no record of WHICH forge issued it. Every token chain here names several
// forges' variables, so "whichever is set" can be a credential issued by a
// different host than the one a caller is about to call. Resolving to a
// Credential instead forces the destination to be named before the secret
// can be read.
type CredentialVar struct{ v Var }

// Keys lists the variable names in precedence order, for flag binding and docs.
func (c CredentialVar) Keys() []string { return c.v.Keys() }

// String renders the chain for help text and error messages.
func (c CredentialVar) String() string { return c.v.String() }

// Resolve finds the first non-empty name in the chain and binds it to the
// audience it may be sent to.
//
// The rule is one line: a token THIS runner injected may only go to this
// runner's own server; a token something else set may go anywhere.
//
// Both halves are load-bearing:
//
//   - Ambient (name.injected(env) is true — the runner we are executing on
//     sets this name): its issuer is that runner's forge, so the audience is
//     ServerURL().ResolveAttested — the runner's own server, read only from
//     what the runner injected. On GitHub that is $GITHUB_SERVER_URL even
//     when a workflow has pointed $FORGEJO_SERVER_URL elsewhere, because the
//     attested traversal skips names this runner does not inject. That is
//     what stops $GITHUB_TOKEN reaching a third-party host.
//   - Deliberate (this runner does NOT inject the name, or nobody does):
//     someone set it on purpose, and only they know what for. $FORGEJO_TOKEN
//     on a GitHub runner is the motivating case — GitHub never sets it, so a
//     workflow that did meant to talk to Forgejo. Restricting it would break
//     the legitimate cross-forge job while protecting nothing.
//
// Order matters and the chain already has it right: the Forgejo-native names
// precede $GITHUB_TOKEN, so a job that sets $FORGEJO_TOKEN gets it rather
// than falling through to an ambient token that would then be denied.
//
// Fails closed: an ambient token whose runner exposes no attested server URL
// gets no audience and is therefore usable nowhere, which degrades to an
// anonymous request rather than guessing a destination.
func (c CredentialVar) Resolve(env func(string) string) Credential {
	for _, name := range c.v.Names {
		secret := env(name.Key)
		if secret == "" {
			continue
		}

		if !name.injected(env) {
			return Credential{secret: secret}
		}

		own, ok := ServerURL().ResolveAttested(env)
		if !ok {
			// The secret exists, but its issuer exposed no attested server
			// URL, so there is nowhere it is known to be valid. Bound with an
			// empty audience: usable nowhere, rather than guessed at. Saying
			// "no credential" instead would be a small lie, and Present() is
			// what callers use to tell "none configured" from "not usable
			// here".
			return Credential{secret: secret, bound: true}
		}

		return Credential{secret: secret, audience: own.String(), bound: true}
	}

	return Credential{}
}

// OperatorCredential wraps a secret an operator supplied EXPLICITLY, such as
// a --token flag value. It is unrestricted: someone typed this credential
// alongside the command it is for, so they, not this package, decided where
// it goes. That is the same judgement the CLI has always made, stated out
// loud instead of implied.
//
// It is the one way to mint a Credential from an arbitrary string, so it is
// also the one way to launder an ambient token past its audience. Only the
// composition root may call it: enforced by
// archguard.TestOnlyCompositionRootMintsOperatorCredentials.
func OperatorCredential(secret string) Credential { return Credential{secret: secret} }

// Credential is a secret together with the destination it may be sent to.
//
// The type exists to make a confused deputy impossible to write by accident.
// A caller holds authority (the secret) and picks a target (a URL); nothing
// in a pair of strings says the two must agree, and that gap was a real
// disclosure: a GitHub-runner job checking out from a third-party Forgejo
// with $FORGEJO_TOKEN unset sent GitHub's job token to that host as HTTP
// Basic auth. There is no method that returns the secret without being told
// where it is going.
//
// String/GoString redact, so a Credential cannot leak through a %v, a log
// line, or a struct dump.
type Credential struct {
	secret string

	// audience is the origin the secret may be sent to. Meaningful only
	// when bound; an unbound Credential was set deliberately and its holder,
	// not this package, knows its intended destination.
	audience string
	bound    bool
}

// Present reports whether a secret was found at all, without revealing it.
// A caller that only needs to say "no token configured" uses this and never
// has to name a destination.
func (c Credential) Present() bool { return c.secret != "" }

// For returns the secret if it may be sent to destination, and "" otherwise.
//
// Empty means "proceed anonymously", not "error": an unauthenticated fetch of
// a public repository is a normal outcome, and the alternative — erroring —
// would turn a tightened credential rule into an outage for every anonymous
// path. Callers that require authentication already fail on the empty string.
//
// destination is any absolute URL; only its origin (scheme, host, port) is
// compared. A destination that does not parse as an absolute URL — an SSH
// remote such as git@forgejo.example.com:o/r.git — matches nothing, which is
// correct: HTTP credentials have no meaning there.
func (c Credential) For(destination string) string {
	if c.secret == "" {
		return ""
	}

	if !c.bound {
		return c.secret
	}

	if sameOrigin(c.audience, destination) {
		return c.secret
	}

	return ""
}

// String redacts. A Credential must never render its secret.
func (c Credential) String() string {
	if c.secret == "" {
		return "<no credential>"
	}

	return "<redacted credential>"
}

// GoString redacts under %#v too.
func (c Credential) GoString() string { return "runcontext.Credential{" + c.String() + "}" }

// sameOrigin reports whether two absolute URLs share scheme, host and port.
//
// It compares PARSED origins rather than strings on purpose. A prefix or
// substring test would accept https://github.com.evil.example for an audience
// of https://github.com — the same class of mistake AnchorIdentity avoids by
// escaping and anchoring its regexp. Both URLs must be absolute and carry a
// host; anything else is not a comparable origin and reports false.
func sameOrigin(audience, destination string) bool {
	issued, err := url.Parse(audience)
	if err != nil || issued.Host == "" || issued.Scheme == "" {
		return false
	}

	target, err := url.Parse(destination)
	if err != nil || target.Host == "" || target.Scheme == "" {
		return false
	}

	return strings.EqualFold(issued.Scheme, target.Scheme) &&
		strings.EqualFold(issued.Host, target.Host)
}

// Tag resolves a release tag. Tag-specific vars win, then the generic
// ref-name fallbacks (a tag push exposes the tag as the ref name).
func Tag() Var {
	return Var{
		Concept: "tag",
		Names: []Name{
			orchestrated("TAG_NAME"),
			orchestrated("RELEASE_TAG"),
			orchestrated("REF_NAME"),
			orchestrated("CI_REF_NAME"),
			forgejoInjected("FORGEJO_REF_NAME"),
			githubInjected("GITHUB_REF_NAME"),
		},
	}
}

// ForgejoRunner reports whether the code is EXECUTING on a Forgejo/Gitea
// Actions runner.
//
// This answers "where am I running", which is a different question from
// "which forge am I talking to" — and conflating them is a bug with two
// heads. It reads only markers a runner injects about ITSELF:
//
//   - $FORGEJO_ACTIONS / $GITEA_ACTIONS: the runner's own "this is me" flag;
//   - $FORGEJO_OUTPUT: the runner-provided step-output sink, a path only a
//     Forgejo runner has any reason to create.
//
// $FORGEJO_SERVER_URL and $FORGEJO_REPOSITORY are deliberately ABSENT even
// though a Forgejo runner does set them, because a workflow on ANY runner
// sets them to name a Forgejo TARGET. Treating them as identity is what let
// a GitHub runner present itself as Forgejo: it suppressed the GitHub
// annotations the run should have emitted, and it made $GITHUB_TOKEN look
// like a Forgejo credential.
//
// The cost of the narrower set is bounded and cosmetic: a Forgejo runner old
// enough to set neither flag nor $FORGEJO_OUTPUT is read as GitHub-dialect,
// so it receives workflow commands it ignores. Noise, not breakage — and the
// opposite error leaks a token.
func ForgejoRunner(env func(string) string) bool {
	// A Forgejo/Gitea runner also sets GITHUB_ACTIONS=true; without that
	// marker we are not on a GitHub-Actions-compatible runner at all.
	// Matched exactly, as platform's ciFlag has always done.
	if env("GITHUB_ACTIONS") != valueTrue {
		return false
	}

	return truthy(env("FORGEJO_ACTIONS")) ||
		truthy(env("GITEA_ACTIONS")) ||
		env("FORGEJO_OUTPUT") != ""
}

// truthy reports whether v holds a conventional truthy value. It matches
// what internal/adapters/platform already accepted, so moving the predicate
// here changes WHICH signals are consulted and nothing else — narrowing the
// accepted spellings would be a separate decision.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Attested* chains: sources for TRUST decisions
//
// The chains above sort by "most deliberate wins": the bare REPOSITORY the
// orchestration layer computes beats the runner's own $GITHUB_REPOSITORY,
// because a value someone set on purpose is the one they meant. That is the
// right order for describing a run — what to build, where to put scratch.
//
// It is the WRONG order for deciding what to trust. A verification anchor
// must sort by "least forgeable wins": the value injected by the runner,
// which nothing upstream had to compute. The two orderings are opposites, so
// one chain cannot serve both, and reusing a descriptive chain for a trust
// decision silently widens what a signature check will accept.
//
// ADR 0002 states the invariant these serve: consumer-supplied code or data
// can never cause scope escalation. Rule 3 shows the shape — the signer takes
// an explicit --expected-image-repository and re-validates against it rather
// than inferring one. An Attested chain is the same idea for the FALLBACK
// used when no expectation was supplied.

// ProjectURL resolves GitLab's project web URL. GitLab injects it and there
// is no neutral alias, so it exists as a concept mainly to give the gitlab
// signing identity an ATTESTED source rather than a bare env read.
func ProjectURL() Var {
	return Var{Concept: "project url", Names: []Name{gitlabInjected("CI_PROJECT_URL")}}
}

// All returns every run-context concept.
//
// It exists so a guard can enumerate the owned names without hand-copying
// them into a second list that would itself drift — the failure this
// package exists to end.
func All() []Var {
	return []Var{
		Repository(), RepositoryOwner(), RefName(), Ref(), RefType(), EventName(),
		Commit(), CheckoutRef(), RunID(), RunURL(), Actor(), ServerURL(),
		TempDir(), Workspace(), tokenVar(), releaseTokenVar(), Tag(), ProjectURL(),
	}
}
