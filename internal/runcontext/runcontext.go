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
// a workflow's `${{ env.X || '' }}` expression must fall through to a
// populated fallback rather than shadow it.
//
// Each chain is the UNION of every variant its consumers previously used,
// so adopting it preserves existing workflows while removing the drift.
package runcontext

import "strings"

// Var is an ordered set of environment variable names that all carry the
// same run-context concept. The first NON-EMPTY name wins.
type Var struct {
	// Concept names what the variable means in the CLI's own vocabulary
	// ("repository", "commit", …) — what an error message calls the thing
	// when none of Names is set.
	Concept string

	// Names is the precedence order, most-preferred first. Never empty.
	Names []string
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
		if value := env(name); value != "" {
			return value
		}
	}

	return ""
}

// String renders the names as "$A / $B / $C".
//
// It exists for "not set" diagnostics: a message built from this lists
// every name that WOULD have worked, instead of naming whichever single
// one the author happened to have in mind — which is how a caller ends up
// told to set $FORGEJO_REPOSITORY when $CI_REPO was the neutral answer.
func (v Var) String() string {
	return "$" + strings.Join(v.Names, " / $")
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
		Names:   []string{"REPOSITORY", "CI_REPO", "FORGEJO_REPOSITORY", "FORGEJO_REPO", "GITHUB_REPOSITORY"},
	}
}

// RepositoryOwner resolves the owning org/user.
func RepositoryOwner() Var {
	return Var{
		Concept: "repository owner",
		Names:   []string{"REPOSITORY_OWNER", "FORGEJO_REPOSITORY_OWNER", "GITHUB_REPOSITORY_OWNER"},
	}
}

// RefName resolves the short git ref name (branch or tag, no refs/ prefix).
func RefName() Var {
	return Var{
		Concept: "ref name",
		Names:   []string{"REF_NAME", "CI_REF_NAME", "FORGEJO_REF_NAME", "GITHUB_REF_NAME"},
	}
}

// Ref resolves the full git ref (refs/heads/…, refs/tags/…).
func Ref() Var {
	return Var{Concept: "ref", Names: []string{"REF", "FORGEJO_REF", "GITHUB_REF"}}
}

// RefType resolves the ref kind ("branch" or "tag").
func RefType() Var {
	return Var{Concept: "ref type", Names: []string{"REF_TYPE", "FORGEJO_REF_TYPE", "GITHUB_REF_TYPE"}}
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
		Names:   []string{"EVENT_NAME", "FORGEJO_EVENT_NAME", "GITHUB_EVENT_NAME"},
	}
}

// Commit resolves the commit SHA. COMMIT_SHA is the bare deliberate name
// the report steps set; it wins over the runner-provided vars but not over
// the CI_COMMIT* pair already in use.
func Commit() Var {
	return Var{
		Concept: "commit",
		Names:   []string{"CI_COMMIT", "CI_COMMIT_SHA", "COMMIT_SHA", "FORGEJO_SHA", "GITHUB_SHA"},
	}
}

// CheckoutRef resolves the ref `platform checkout` materializes: an explicit
// CHECKOUT_REF (branch, tag, or SHA) takes precedence — letting a workflow
// check out a branch via env — and falls back to the triggering commit so
// the default is unchanged.
func CheckoutRef() Var {
	return Var{
		Concept: "checkout ref",
		Names:   []string{"CHECKOUT_REF", "CI_COMMIT", "CI_COMMIT_SHA", "FORGEJO_SHA", "GITHUB_SHA"},
	}
}

// RunID resolves the CI run identifier.
func RunID() Var {
	return Var{Concept: "run id", Names: []string{"CI_RUN_ID", "FORGEJO_RUN_ID", "GITHUB_RUN_ID"}}
}

// RunURL resolves the human-facing CI run URL.
func RunURL() Var {
	return Var{Concept: "run url", Names: []string{"CI_RUN_URL"}}
}

// Actor resolves the triggering user.
//
// The two bindings of this concept had disjoint name lists: the flag chain
// knew only CI_ACTOR while every registry-auth adapter read the runner's
// native FORGEJO_ACTOR/GITHUB_ACTOR, so they shared no name at all and a
// runner-provided actor was invisible to the flag. This is their union.
func Actor() Var {
	return Var{Concept: "actor", Names: []string{"CI_ACTOR", "FORGEJO_ACTOR", "GITHUB_ACTOR"}}
}

// ServerURL resolves the forge base URL (e.g. https://codeberg.org). It
// builds git-over-HTTPS clone URLs, so it must resolve on every forge:
// CI_SERVER_URL (GitLab-native / neutral) → FORGEJO_SERVER_URL → the
// GitHub-native GITHUB_SERVER_URL the Forgejo runner also sets.
//
// FORGEJO_SERVER is the forgejo-ci workflows' explicitly-set alias for the
// runner's FORGEJO_SERVER_URL (same value, `forgejo.server_url`).
func ServerURL() Var {
	return Var{
		Concept: "server url",
		Names:   []string{"CI_SERVER_URL", "FORGEJO_SERVER_URL", "FORGEJO_SERVER", "GITHUB_SERVER_URL"},
	}
}

// TempDir resolves the runner scratch directory.
func TempDir() Var {
	return Var{Concept: "temp dir", Names: []string{"CI_TEMP_DIR", "RUNNER_TEMP"}}
}

// Workspace resolves the checkout target directory.
func Workspace() Var {
	return Var{
		Concept: "workspace",
		Names:   []string{"CI_WORKSPACE", "FORGEJO_WORKSPACE", "GITHUB_WORKSPACE"},
	}
}

// Token resolves the forge API/clone token for the forge this run BELONGS
// to — the one whose runner injected the credential.
//
// Non-empty semantics matter: an empty GITHUB_TOKEN must not shadow a
// populated FORGEJO_TOKEN, and an empty result means an anonymous
// (public-repo) checkout.
//
// GITEA_TOKEN is the Gitea-native name that Forgejo still accepts.
//
// # This chain spans forges, and that is safe ONLY here
//
// A runner injects its own token name and no other's, so in a normal run at
// most one of these is set and the chain simply finds it. It is NOT safe as
// a general "give me a token": if a workflow sets more than one — a job
// publishing across forges — this returns whichever comes first, which may
// be a credential issued by a DIFFERENT host than the one you are about to
// call.
//
// So do not reach for this when you know the destination. Use
// TokenForForgejo, or read the forge's own name, and see the note on
// github/forgeregistry.go's Token field.
func Token() Var {
	return Var{Concept: conceptToken, Names: append(forgeNeutralTokenNames(), nameGitHubToken)}
}

// conceptToken labels both token chains: they name the same concept, only
// scoped differently.
const conceptToken = "token"

// nameGitHubToken is the one name whose meaning depends on WHERE we run: a
// Forgejo runner injects the job token under it as a GitHub-compat alias,
// while on GitHub it is GitHub's own. Every chain that includes it has to
// say why.
const nameGitHubToken = "GITHUB_TOKEN" //nolint:gosec // G101: an env var NAME; this package holds names, never values.

// forgeNeutralTokenNames are the names that mean the same thing on any
// runner: CI_TOKEN is set deliberately by the workflow, and the Forgejo and
// Gitea names are unambiguous about which forge they authenticate to.
//
// Returned fresh so callers can append without aliasing a shared backing
// array — and declared once so the two chains below cannot drift.
func forgeNeutralTokenNames() []string {
	return []string{"CI_TOKEN", "FORGEJO_TOKEN", "GITEA_TOKEN"}
}

// TokenForForgejo resolves a credential valid at a FORGEJO server, given the
// runner we are executing on.
//
// $GITHUB_TOKEN is admitted only when the Forgejo runner itself injected it:
// act_runner exposes the job token under the GitHub-compatible name, so
// there it IS the Forgejo credential. On any other runner $GITHUB_TOKEN is
// GitHub's own job token — useless at a Forgejo server (it cannot
// authenticate) but perfectly capable of being transmitted to it.
//
// That asymmetry is the whole point: the fallback has only two possible
// outcomes off a Forgejo runner — an auth failure, or a GitHub credential
// disclosed to a third-party host. Dropping it there costs no working case.
//
// The neutral and Forgejo-native names need no gate: a workflow that sets
// CI_TOKEN or FORGEJO_TOKEN while targeting Forgejo has said what it means.
func TokenForForgejo(env func(string) string) Var {
	names := forgeNeutralTokenNames()
	if ForgejoRunner(env) {
		names = append(names, nameGitHubToken)
	}

	return Var{Concept: conceptToken, Names: names}
}

// ReleaseToken resolves the write-scoped token used to push the release
// commit and tag. The dedicated RELEASE_TOKEN wins (least-privilege: a
// separate write token, distinct from the read-only clone token), then the
// same forge-generic chain as Token so the push works on any forge. Empty
// semantics matter here too: an unset RELEASE_TOKEN (a `${{ secrets.* }}`
// that resolved to "") must fall through, not shadow the forge's ambient
// token.
// It carries the same forge-spanning caveat as Token, and is built from that
// chain rather than restating it, so the two cannot drift.
func ReleaseToken() Var {
	return Var{
		Concept: "release token",
		Names:   append([]string{"RELEASE_TOKEN"}, Token().Names...),
	}
}

// Tag resolves a release tag. Tag-specific vars win, then the generic
// ref-name fallbacks (a tag push exposes the tag as the ref name).
func Tag() Var {
	return Var{
		Concept: "tag",
		Names: []string{
			"TAG_NAME", "RELEASE_TAG", "REF_NAME", "CI_REF_NAME", "FORGEJO_REF_NAME", "GITHUB_REF_NAME",
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
	if env("GITHUB_ACTIONS") != "true" {
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

// All returns every run-context concept.
//
// It exists so a guard can enumerate the owned names without hand-copying
// them into a second list that would itself drift — the failure this
// package exists to end.
func All() []Var {
	return []Var{
		Repository(), RepositoryOwner(), RefName(), Ref(), RefType(), EventName(),
		Commit(), CheckoutRef(), RunID(), RunURL(), Actor(), ServerURL(),
		TempDir(), Workspace(), Token(), ReleaseToken(), Tag(),
	}
}
