// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/clicolor"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// TokenInput drives `validate auth token`. Forge-specific labels and
// token-format advice come from the provider itself (Describer /
// TokenAdviser), not from a platform field here.
type TokenInput struct {
	Token      string
	Repository string
}

// Token validates a release-bot token. Forge-specific behaviour is
// delegated to the provider: labels and setup guidance come from its
// Describer; token-format advice (e.g. GitHub classic-PAT refusal)
// comes from its optional TokenAdviser. Validity is confirmed by the
// provider's ValidateToken probe.
func Token(ctx context.Context, prov provider.TokenValidator, out io.Writer, in TokenInput) error {
	info := describe(prov)

	if in.Token == "" {
		return fmt.Errorf(
			"no %s token provided\n%s: %w",
			info.DisplayName,
			tokenSetupGuidance(info),
			errs.ErrPermissionDenied)
	}

	if in.Repository == "" {
		return fmt.Errorf("no repository provided\nusage: validate auth token <token> <repository>: %w", errs.ErrUsage)
	}

	if adviser, ok := prov.(provider.TokenAdviser); ok {
		advice, reject := adviser.AdviseToken(in.Token)
		if reject {
			return fmt.Errorf("%s: %w", advice, errs.ErrPermissionDenied)
		}

		if advice != "" {
			_, _ = fmt.Fprintf(out, "%s\n", advice)
		}
	}

	if err := prov.ValidateToken(ctx, in.Token, in.Repository); err != nil {
		return fmt.Errorf(
			"Token is invalid or lacks permissions\n"+
				"%s\n"+
				"Ensure the token has 'contents: write' permission for this repository: %w",
			err, errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "%s %s token validated\n", clicolor.Check(out), info.DisplayName)

	return nil
}

// describe returns the provider's self-description, falling back to a
// generic label when the dependency does not implement Describer (every
// real adapter does).
func describe(prov provider.TokenValidator) provider.Info {
	if d, ok := prov.(provider.Describer); ok {
		return d.Describe()
	}

	return provider.Info{
		DisplayName: "the configured",
		ScopesHint:  "Provide a token with the appropriate permissions.",
	}
}

// tokenSetupGuidance renders the human guidance line from a provider's
// self-description: the scopes hint, plus a "create one at" pointer when
// the provider exposes a setup URL.
func tokenSetupGuidance(info provider.Info) string {
	if info.SetupURL == "" {
		return info.ScopesHint
	}

	return info.ScopesHint + "\nCreate one at: " + info.SetupURL
}

// BotPermissionsInput drives `validate auth bot-permissions`.
type BotPermissionsInput struct {
	Repository string
}

// BotPermissions runs the three-probe check and renders the canonical
// w sequence. RepoAccessible=false is fatal; UserAccessible=false
// is also fatal; BranchesAccessible=false produces a warn-only block.
func BotPermissions(ctx context.Context, prov provider.TokenValidator, out io.Writer, in BotPermissionsInput) error {
	if in.Repository == "" {
		return fmt.Errorf("usage: validate auth bot-permissions <repository>: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(out, "Validating bot token permissions...\n")

	bp, err := prov.ValidateBotPermissions(ctx, in.Repository)
	if err != nil {
		return fmt.Errorf("probe bot permissions: %w", err)
	}

	if !bp.UserAccessible {
		return fmt.Errorf("RELEASE_TOKEN is invalid or expired: %w", errs.ErrPermissionDenied)
	}

	if !bp.RepoAccessible {
		return fmt.Errorf("bot token cannot access this repository\nplease ensure the bot has appropriate repository access: %w", errs.ErrPermissionDenied)
	}

	if !bp.BranchesAccessible {
		_, _ = fmt.Fprintf(out, "⚠️  Bot token may have limited permissions\n")
		_, _ = fmt.Fprintf(out, "Ensure the bot has sufficient permissions to:\n")
		_, _ = fmt.Fprintf(out, "  - Push commits to branches\n")
		_, _ = fmt.Fprintf(out, "  - Create and move tags\n")
		_, _ = fmt.Fprintf(out, "  - Bypass branch protection (if enabled)\n")
	}

	_, _ = fmt.Fprintf(out, "%s Bot token is valid and has repository access\n", clicolor.Check(out))

	return nil
}

// Release authorisation lives at the tag-signature layer: see
// validate.tags.go::TagSignature + checkGPGSignerAllowlist +
// checkSSHSignerAllowlist. The committed allowlist files are
// .reusable-ci/allowed_signers (SSH) and
// .reusable-ci/allowed_gpg_keys.asc (GPG); contract documented in
// docs/verification.md.
