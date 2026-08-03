// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cienv binds the run-context concepts declared in
// internal/runcontext to urfave/cli flag sources, so a command declares
// `Sources: cienv.Repository()` instead of an inline cli.EnvVars(...) chain.
//
// This package owns NO variable names. It owns the BINDING only: which
// names mean "the repository", and in what precedence, is runcontext's job.
// The split is deliberate. The layering forbids adapters->cli and app->cli,
// so name lists that lived here were unreachable from half the hexagon;
// those layers re-declared them inline and the lists drifted apart — the
// very drift this package was created to end. Names are shared knowledge
// and live in a leaf; only their translation into urfave's types is a CLI
// concern, and that is what remains here.
//
// The precedence convention and the non-empty rule are documented on
// runcontext, since that is where they are decided.
package cienv

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// nonEmptyEnvSource is a cli.ValueSource that treats a set-but-EMPTY
// environment variable as ABSENT. urfave/cli's built-in env source resolves
// via os.LookupEnv, which reports a variable set to "" as found — so a
// forge-neutral var blanked by a workflow's `${{ env.X || ” }}` expression
// would short-circuit the chain and shadow a populated fallback. Skipping
// empties makes the next source win, which is what every call site wants.
//
// This mirrors runcontext.Var.Resolve, which skips empties for the same
// reason: the two bindings of a concept must agree on what "absent" means.
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

// Sources binds a run-context concept to a non-empty env ValueSourceChain,
// preserving its precedence order.
func Sources(v runcontext.Var) cli.ValueSourceChain {
	keys := v.Keys()

	srcs := make([]cli.ValueSource, len(keys))
	for i, k := range keys {
		srcs[i] = nonEmptyEnvSource{key: k}
	}

	return cli.NewValueSourceChain(srcs...)
}

// One binding per run-context concept. Each is a thin delegation: the names
// and their order are documented on the corresponding runcontext function.

// Repository resolves "owner/repo".
func Repository() cli.ValueSourceChain { return Sources(runcontext.Repository()) }

// RepositoryOwner resolves the owning org/user.
func RepositoryOwner() cli.ValueSourceChain { return Sources(runcontext.RepositoryOwner()) }

// RefName resolves the short git ref name (branch or tag, no refs/ prefix).
func RefName() cli.ValueSourceChain { return Sources(runcontext.RefName()) }

// Ref resolves the full git ref (refs/heads/…, refs/tags/…).
func Ref() cli.ValueSourceChain { return Sources(runcontext.Ref()) }

// RefType resolves the ref kind ("branch" or "tag").
func RefType() cli.ValueSourceChain { return Sources(runcontext.RefType()) }

// EventName resolves the workflow trigger event in the canonical vocabulary.
func EventName() cli.ValueSourceChain { return Sources(runcontext.EventName()) }

// Commit resolves the commit SHA.
func Commit() cli.ValueSourceChain { return Sources(runcontext.Commit()) }

// CheckoutRef resolves the ref `platform checkout` materializes.
func CheckoutRef() cli.ValueSourceChain { return Sources(runcontext.CheckoutRef()) }

// RunID resolves the CI run identifier.
func RunID() cli.ValueSourceChain { return Sources(runcontext.RunID()) }

// RunURL resolves the human-facing CI run URL.
func RunURL() cli.ValueSourceChain { return Sources(runcontext.RunURL()) }

// Actor resolves the triggering user.
func Actor() cli.ValueSourceChain { return Sources(runcontext.Actor()) }

// ServerURL resolves the forge base URL (e.g. https://codeberg.org).
func ServerURL() cli.ValueSourceChain { return Sources(runcontext.ServerURL()) }

// TempDir resolves the runner scratch directory.
func TempDir() cli.ValueSourceChain { return Sources(runcontext.TempDir()) }

// Workspace resolves the checkout target directory.
func Workspace() cli.ValueSourceChain { return Sources(runcontext.Workspace()) }

// There is deliberately NO Token()/ReleaseToken() binding here.
//
// A ValueSourceChain yields a string, and by then the credential has lost the
// one fact that makes it safe to send: which forge issued it. Auto-filling
// --token from the chain was a real disclosure — on a GitHub runner checking
// out from a third-party Forgejo with $FORGEJO_TOKEN unset, --server-url
// resolved to the Forgejo host while --token fell through to $GITHUB_TOKEN,
// and the two never had to agree.
//
// Tokens are resolved in the command's Action instead, via
// runcontext.Token().Resolve(os.Getenv), which returns a Credential bound to
// the destination it may be sent to. The env var names still reach --help and
// the generated reference through each flag's Usage text.

// Tag resolves a release tag.
func Tag() cli.ValueSourceChain { return Sources(runcontext.Tag()) }
