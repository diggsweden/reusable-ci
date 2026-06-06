// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"fmt"
	"io"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// TokenInput drives `validate auth token` on the GitHub or GitLab provider.
type TokenInput struct {
	Token      string
	Repository string
	// Platform is the active CI provider — only used for guidance text
	// (label, token-format hints). The actual probe runs against the
	// TokenValidator dependency, regardless of platform.
	Platform provider.Platform
}

// Token validates a release-bot token. On GitHub it gates classic PATs
// (ghp_*) and surfaces an info line for unknown prefixes; validity is
// confirmed by a single authenticated GET against the repo. On GitLab
// the format checks are skipped (no canonical token kinds) and the
// network call alone reports validity.
func Token(ctx context.Context, prov provider.TokenValidator, out io.Writer, in TokenInput) error {
	if in.Token == "" {
		return fmt.Errorf(
			"no %s token provided\n%s: %w",
			tokenKindLabel(in.Platform),
			tokenSetupGuidance(in.Platform),
			errs.ErrPermissionDenied)
	}

	if in.Repository == "" {
		return fmt.Errorf("no repository provided\nusage: validate auth token <token> <repository>: %w", errs.ErrUsage)
	}

	if in.Platform == provider.PlatformGitHub {
		switch validate.ClassifyGitHubToken(in.Token) {
		case validate.GitHubTokenClassic:
			return fmt.Errorf("classic PAT detected (ghp_*)\n"+
				"Classic PATs have broad access and are not recommended.\n"+
				"Please use a fine-grained PAT (github_pat_*) with 'contents: write' permission.\n"+
				"See: https://github.com/settings/personal-access-tokens/new: %w", errs.ErrPermissionDenied)
		case validate.GitHubTokenUnknown:
			_, _ = fmt.Fprintf(out, "ℹ️  Unknown token type. Expected fine-grained PAT (github_pat_*) or GitHub App token (ghs_*).\n")
		default:
			// GitHubTokenFineGrained / GitHubTokenApp: the recommended
			// shapes — pass silently to the API-side ValidateToken probe.
		}
	}

	if err := prov.ValidateToken(ctx, in.Token, in.Repository); err != nil {
		return fmt.Errorf(
			"Token is invalid or lacks permissions\n"+
				"%s\n"+
				"Ensure the token has 'contents: write' permission for this repository: %w",
			err, errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "✓ %s token validated\n", tokenKindLabel(in.Platform))

	return nil
}

func tokenKindLabel(p provider.Platform) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch p {
	case provider.PlatformGitHub:
		return "GitHub"
	case provider.PlatformGitLab:
		return "GitLab"
	default:
		// PlatformLocal (or any unknown): use the raw enum value as
		// label — local mode never reaches token-validation use cases.
		return string(p)
	}
}

func tokenSetupGuidance(p provider.Platform) string {
	switch p {
	case provider.PlatformGitHub:
		return "A fine-grained PAT (github_pat_*) with 'contents: write' permission is required.\n" +
			"Create one at: https://github.com/settings/personal-access-tokens/new"
	case provider.PlatformGitLab:
		return "A project / group / personal access token with api + write_repository scopes is required.\n" +
			"Create one at: https://gitlab.com/-/user_settings/personal_access_tokens"
	default:
		// PlatformLocal (or any unknown): generic guidance — local mode
		// never reaches token-setup paths.
		return "Provide a token with the appropriate permissions."
	}
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

	_, _ = fmt.Fprintf(out, "✓ Bot token is valid and has repository access\n")

	return nil
}

// Release authorisation lives at the tag-signature layer: see
// validate.tags.go::TagSignature + checkGPGSignerAllowlist +
// checkSSHSignerAllowlist. The committed allowlist files are
// .reusable-ci/allowed_signers (SSH) and
// .reusable-ci/allowed_gpg_fingerprints (GPG); contract documented in
// docs/verification.md.
