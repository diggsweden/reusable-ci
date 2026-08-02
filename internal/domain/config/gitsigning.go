// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// GitSignMethod selects how the release commit and tag (git objects) are
// signed. It is independent of SignConfig.Method, which selects how release
// *artifacts* are signed: a repo can sign artifacts keyless (sigstore) while
// still signing its git tag with gpg or ssh.
type GitSignMethod string

// Recognised GitSignMethod values. gitsign (Sigstore git signing) is
// deliberately omitted — SSH signing is git-native, forge-portable, and
// verifiable via .reusable-ci/allowed_signers, which covers the
// no-key-on-runner goal without the extra dependency.
const (
	GitSignGPG GitSignMethod = "gpg"
	GitSignSSH GitSignMethod = "ssh"
)

// DefaultGitSignMethod is what the release flow uses when `git-signing` is
// absent or its method is empty. gpg preserves the existing contract: repos
// that never configured git-signing keep signing tags/commits with their GPG
// key.
const DefaultGitSignMethod = GitSignGPG

// ValidGitSignMethods lists every GitSignMethod in canonical order. It
// is the single definition of the set: Validate accepts exactly these
// values and the generated artifacts.yml JSON Schema renders its
// git-signing.method enum from this slice.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidGitSignMethods = []GitSignMethod{
	GitSignGPG,
	GitSignSSH,
}

// GitSigningConfig is the top-level `git-signing:` block in artifacts.yml.
// All fields optional; an empty block (or empty Method) defaults to gpg.
type GitSigningConfig struct {
	Method GitSignMethod `yaml:"method,omitempty"`
}

// EffectiveMethod returns the configured method or the package default when
// none is set. Callers that need to know whether the operator made an explicit
// choice should inspect GitSigningConfig.Method directly.
func (g GitSigningConfig) EffectiveMethod() GitSignMethod {
	if g.Method == "" {
		return DefaultGitSignMethod
	}

	return g.Method
}

// Validate enforces that the method is one of ValidGitSignMethods (or
// empty → gpg). Errors wrap errs.ErrInvalidConfig so the CLI exits
// EX_CONFIG (78).
func (g GitSigningConfig) Validate() error {
	if slices.Contains(ValidGitSignMethods, g.EffectiveMethod()) {
		return nil
	}

	parts := make([]string, len(ValidGitSignMethods))
	for i, method := range ValidGitSignMethods {
		parts[i] = string(method)
	}

	return fmt.Errorf(
		"git-signing.method %q is not one of [%s]: %w",
		g.Method, strings.Join(parts, ", "), errs.ErrInvalidConfig)
}
