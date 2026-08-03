// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package clitoken resolves a command's token flag into a
// runcontext.Credential.
//
// It is the composition root's one credential-minting seam. It exists as its
// own package so the rule "only the composition root turns a string into an
// unrestricted credential" is a single, greppable call site rather than a
// convention repeated in every command.
package clitoken

import (
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// Resolve returns the credential a command should use.
//
// An explicit --token wins and is unrestricted: the operator typed a secret
// next to the command it is for, so they decided where it goes. That is the
// judgement the CLI has always made; it is now stated rather than implied.
//
// Otherwise the runner's own token is used, bound to the server that issued
// it. These flags deliberately declare no env Sources: a ValueSourceChain
// yields a string, and a string cannot remember which forge issued it -- the
// gap that let $GITHUB_TOKEN auto-fill --token for a checkout aimed at a
// third-party Forgejo host.
func Resolve(cmd *cli.Command) runcontext.Credential {
	if v := cmd.String("token"); v != "" {
		return runcontext.OperatorCredential(v)
	}

	return runcontext.Token().Resolve(os.Getenv)
}

// ResolveRelease is Resolve for the write-scoped release token chain, which
// prefers a dedicated $RELEASE_TOKEN before the forge's ambient one.
func ResolveRelease(cmd *cli.Command) runcontext.Credential {
	if v := cmd.String("token"); v != "" {
		return runcontext.OperatorCredential(v)
	}

	return runcontext.ReleaseToken().Resolve(os.Getenv)
}
