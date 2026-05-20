// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cienv centralizes the forge-neutral environment variables that
// feed CLI flags, so every command resolves a given run-context concept
// (repository, ref name, commit, run id, …) from the SAME ordered set of
// sources with ONE precedence rule. Before this package each command
// hand-rolled its own cli.EnvVars(...) chain, which drifted: the same
// concept was read from different vars, in different precedence orders, in
// different commands.
//
// Precedence convention (first NON-EMPTY source wins):
//
//  1. the project-neutral BARE name the reusable-ci workflows set
//     explicitly (e.g. REPOSITORY, REF_NAME) — the deliberately-computed
//     value the orchestration layer wants the command to use;
//  2. the GitLab/Forgejo-native CI_*/FORGEJO_* name (runner-provided);
//  3. the GitHub-native GITHUB_* name (runner-provided).
//
// Each chain is the UNION of every variant the commands previously used, so
// migrating to it preserves every existing workflow flow while removing the
// drift.
package cienv

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

// nonEmptyEnvSource is a cli.ValueSource that treats a set-but-EMPTY
// environment variable as ABSENT. urfave/cli's built-in env source resolves
// via os.LookupEnv, which reports a variable set to "" as found — so a
// forge-neutral var blanked by a workflow's `${{ env.X || ” }}` expression
// would short-circuit the chain and shadow a populated fallback. Skipping
// empties makes the next source win, which is what every call site wants.
//
// It implements cli.EnvValueSource (IsFromEnv/Key) so `--help` and the
// generated CLI reference still enumerate the keys.
type nonEmptyEnvSource struct{ key string }

func (s nonEmptyEnvSource) Lookup() (string, bool) {
	v, ok := os.LookupEnv(s.key)
	if !ok || v == "" {
		return "", false
	}

	return v, true
}

func (s nonEmptyEnvSource) IsFromEnv() bool  { return true }
func (s nonEmptyEnvSource) Key() string      { return s.key }
func (s nonEmptyEnvSource) String() string   { return fmt.Sprintf("environment variable %q", s.key) }
func (s nonEmptyEnvSource) GoString() string { return fmt.Sprintf("&nonEmptyEnvSource{Key:%q}", s.key) }

// vars builds a non-empty env ValueSourceChain in precedence order.
func vars(keys ...string) cli.ValueSourceChain {
	srcs := make([]cli.ValueSource, len(keys))
	for i, k := range keys {
		srcs[i] = nonEmptyEnvSource{key: k}
	}

	return cli.NewValueSourceChain(srcs...)
}

// Run-context concept sources. One definition per concept — every command
// references these instead of an inline cli.EnvVars(...) so precedence and
// fallbacks can never drift again.

// Repository resolves "owner/repo".
//
// As with every chain below that falls back to a $GITHUB_* var: Forgejo's
// native $FORGEJO_* name is preferred ahead of the $GITHUB_* compat alias.
// Forgejo Runner 7.0.0 lets workflows drop the GITHUB_ names entirely, so
// relying on the alias alone is fragile; on GitHub the FORGEJO_* var is
// unset, so the alias still wins there.
func Repository() cli.ValueSourceChain {
	return vars("REPOSITORY", "CI_REPO", "FORGEJO_REPOSITORY", "GITHUB_REPOSITORY")
}

// RepositoryOwner resolves the owning org/user.
func RepositoryOwner() cli.ValueSourceChain {
	return vars("REPOSITORY_OWNER", "FORGEJO_REPOSITORY_OWNER", "GITHUB_REPOSITORY_OWNER")
}

// RefName resolves the short git ref name (branch or tag, no refs/ prefix).
func RefName() cli.ValueSourceChain {
	return vars("REF_NAME", "CI_REF_NAME", "FORGEJO_REF_NAME", "GITHUB_REF_NAME")
}

// Ref resolves the full git ref (refs/heads/…, refs/tags/…).
func Ref() cli.ValueSourceChain { return vars("REF", "FORGEJO_REF", "GITHUB_REF") }

// RefType resolves the ref kind ("branch" or "tag").
func RefType() cli.ValueSourceChain { return vars("REF_TYPE", "FORGEJO_REF_TYPE", "GITHUB_REF_TYPE") }

// Commit resolves the commit SHA.
func Commit() cli.ValueSourceChain {
	return vars("CI_COMMIT", "CI_COMMIT_SHA", "FORGEJO_SHA", "GITHUB_SHA")
}

// CheckoutRef resolves the ref `platform checkout` materializes: an explicit
// CHECKOUT_REF (branch, tag, or SHA) takes precedence — letting a workflow
// check out a branch via env — and falls back to the triggering commit so the
// default is unchanged.
func CheckoutRef() cli.ValueSourceChain {
	return vars("CHECKOUT_REF", "CI_COMMIT", "CI_COMMIT_SHA", "FORGEJO_SHA", "GITHUB_SHA")
}

// RunID resolves the CI run identifier.
func RunID() cli.ValueSourceChain { return vars("CI_RUN_ID", "FORGEJO_RUN_ID", "GITHUB_RUN_ID") }

// RunURL resolves the human-facing CI run URL.
func RunURL() cli.ValueSourceChain { return vars("CI_RUN_URL") }

// Actor resolves the triggering user.
func Actor() cli.ValueSourceChain { return vars("CI_ACTOR") }

// ServerURL resolves the forge base URL (e.g. https://codeberg.org). It
// builds git-over-HTTPS clone URLs, so it must resolve on every forge:
// CI_SERVER_URL (GitLab-native / neutral) → FORGEJO_SERVER_URL → the
// GitHub-native GITHUB_SERVER_URL the Forgejo runner also sets.
func ServerURL() cli.ValueSourceChain {
	return vars("CI_SERVER_URL", "FORGEJO_SERVER_URL", "GITHUB_SERVER_URL")
}

// TempDir resolves the runner scratch directory.
func TempDir() cli.ValueSourceChain { return vars("CI_TEMP_DIR", "RUNNER_TEMP") }

// Workspace resolves the checkout target directory.
func Workspace() cli.ValueSourceChain {
	return vars("CI_WORKSPACE", "FORGEJO_WORKSPACE", "GITHUB_WORKSPACE")
}

// Token resolves the forge API/clone token. Non-empty semantics matter:
// an empty GITHUB_TOKEN must not shadow a populated FORGEJO_TOKEN, and an
// empty result means an anonymous (public-repo) checkout.
func Token() cli.ValueSourceChain { return vars("CI_TOKEN", "FORGEJO_TOKEN", "GITHUB_TOKEN") }

// ReleaseToken resolves the write-scoped token used to push the release
// commit and tag. The dedicated RELEASE_TOKEN wins (least-privilege: a
// separate write token, distinct from the read-only clone token), then the
// same forge-generic chain as Token() so the push works on any forge. Empty
// semantics matter here too: an unset RELEASE_TOKEN (a `${{ secrets.* }}`
// that resolved to "") must fall through, not shadow the forge's ambient
// token.
func ReleaseToken() cli.ValueSourceChain {
	return vars("RELEASE_TOKEN", "CI_TOKEN", "FORGEJO_TOKEN", "GITHUB_TOKEN")
}

// Tag resolves a release tag. Tag-specific vars win, then the generic
// ref-name fallbacks (a tag push exposes the tag as the ref name).
func Tag() cli.ValueSourceChain {
	return vars("TAG_NAME", "RELEASE_TAG", "REF_NAME", "CI_REF_NAME", "FORGEJO_REF_NAME", "GITHUB_REF_NAME")
}
