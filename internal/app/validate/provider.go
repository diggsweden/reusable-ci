// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// TokenInput drives `validate token` on the GitHub or GitLab provider.
type TokenInput struct {
	Token      string
	Repository string
}

// Token validates a release-bot token. On GitHub it gates classic PATs
// (ghp_*) and surfaces an info line for unknown prefixes; the network
// call is the same single GET that the bash script makes. On GitLab the
// format checks are skipped (no canonical token kinds) and the network
// call alone reports validity.
func Token(ctx context.Context, prov provider.Provider, out io.Writer, in TokenInput) error {
	if in.Token == "" {
		return fmt.Errorf(
			"No %s token provided\n%s: %w",
			tokenKindLabel(prov.Name()),
			tokenSetupGuidance(prov.Name()),
			errs.ErrPermissionDenied)
	}
	if in.Repository == "" {
		return fmt.Errorf("No repository provided\nUsage: validate token <token> <repository>: %w", errs.ErrUsage)
	}

	if prov.Name() == provider.PlatformGitHub {
		switch validate.ClassifyGitHubToken(in.Token) {
		case validate.GitHubTokenClassic:
			return fmt.Errorf("Classic PAT detected (ghp_*)\n"+
				"Classic PATs have broad access and are not recommended.\n"+
				"Please use a fine-grained PAT (github_pat_*) with 'contents: write' permission.\n"+
				"See: https://github.com/settings/personal-access-tokens/new: %w", errs.ErrPermissionDenied)
		case validate.GitHubTokenUnknown:
			fmt.Fprintf(out, "ℹ️  Unknown token type. Expected fine-grained PAT (github_pat_*) or GitHub App token (ghs_*).\n")
		}
	}

	if err := prov.ValidateToken(ctx, in.Token, in.Repository); err != nil {
		return fmt.Errorf(
			"Token is invalid or lacks permissions\n"+
				"%s\n"+
				"Ensure the token has 'contents: write' permission for this repository: %w",
			err, errs.ErrPermissionDenied)
	}
	fmt.Fprintf(out, "✓ %s token validated\n", tokenKindLabel(prov.Name()))
	return nil
}

func tokenKindLabel(p provider.Platform) string {
	switch p {
	case provider.PlatformGitHub:
		return "GitHub"
	case provider.PlatformGitLab:
		return "GitLab"
	}
	return string(p)
}

func tokenSetupGuidance(p provider.Platform) string {
	switch p {
	case provider.PlatformGitHub:
		return "A fine-grained PAT (github_pat_*) with 'contents: write' permission is required.\n" +
			"Create one at: https://github.com/settings/personal-access-tokens/new"
	case provider.PlatformGitLab:
		return "A project / group / personal access token with api + write_repository scopes is required.\n" +
			"Create one at: https://gitlab.com/-/user_settings/personal_access_tokens"
	}
	return "Provide a token with the appropriate permissions."
}

// BotPermissionsInput drives `validate bot-permissions`.
type BotPermissionsInput struct {
	Repository string
}

// BotPermissions runs the three-probe check and renders the bash's
// stdout sequence. RepoAccessible=false is fatal; UserAccessible=false
// is also fatal (mirrors the bash's "RELEASE_TOKEN is invalid or
// expired"); BranchesAccessible=false produces a warn-only block.
func BotPermissions(ctx context.Context, prov provider.Provider, out io.Writer, in BotPermissionsInput) error {
	if in.Repository == "" {
		return fmt.Errorf("Usage: validate bot-permissions <repository>: %w", errs.ErrUsage)
	}
	fmt.Fprintf(out, "Validating bot token permissions...\n")

	bp, err := prov.ValidateBotPermissions(ctx, in.Repository)
	if err != nil {
		return fmt.Errorf("probe bot permissions: %w", err)
	}
	if !bp.UserAccessible {
		return fmt.Errorf("RELEASE_TOKEN is invalid or expired: %w", errs.ErrPermissionDenied)
	}
	if !bp.RepoAccessible {
		return fmt.Errorf("Bot token cannot access this repository\nPlease ensure the bot has appropriate repository access: %w", errs.ErrPermissionDenied)
	}
	if !bp.BranchesAccessible {
		fmt.Fprintf(out, "⚠️  Bot token may have limited permissions\n")
		fmt.Fprintf(out, "Ensure the bot has sufficient permissions to:\n")
		fmt.Fprintf(out, "  - Push commits to branches\n")
		fmt.Fprintf(out, "  - Create and move tags\n")
		fmt.Fprintf(out, "  - Bypass branch protection (if enabled)\n")
	}
	fmt.Fprintf(out, "✓ Bot token is valid and has repository access\n")
	return nil
}

// AuthorizationInput drives `validate authorization`.
type AuthorizationInput struct {
	Tag            string
	Actor          string
	AuthorizedDevs string // CSV
}

// Authorization runs the production-release gate. Order:
//
//	SNAPSHOT   → bypass (zero exit, info-only message)
//	no devs    → no-restrictions (warning, zero exit)
//	in devs    → allowed (info, zero exit)
//	otherwise  → denied (non-zero exit with bash-equivalent guidance)
func Authorization(out io.Writer, in AuthorizationInput) error {
	if in.Tag == "" || in.Actor == "" {
		return fmt.Errorf("Usage: validate authorization <tag-name> <actor> [authorized-devs]: %w", errs.ErrUsage)
	}
	d := validate.DecideAuthorization(in.Tag, in.Actor, in.AuthorizedDevs)
	switch d.Outcome {
	case validate.AuthorizationSnapshotBypass:
		fmt.Fprintf(out, "− SNAPSHOT release - authorization check skipped\n")
		return nil
	case validate.AuthorizationNoRestrictions:
		fmt.Fprintf(out, "⚠️  RELEASE_AUTHORIZED_USERS secret not configured\n")
		fmt.Fprintf(out, "All users with tag push access can create releases\n")
		fmt.Fprintf(out, "✓ Authorization check passed (no restrictions configured)\n")
		return nil
	case validate.AuthorizationAllowed:
		fmt.Fprintf(out, "✓ User '%s' is authorized to create production releases\n", d.Actor)
		return nil
	}
	// Denied
	var b strings.Builder
	fmt.Fprintf(&b, "User '%s' is not authorized to create non-SNAPSHOT releases\n", d.Actor)
	b.WriteString("Only the following users can create production releases:\n")
	for _, u := range d.AuthorizedUsers {
		fmt.Fprintf(&b, "  - %s\n", u)
	}
	b.WriteString("\nIf you need to create a release, please:\n")
	b.WriteString("1. Contact one of the authorized developers\n")
	b.WriteString("2. Or create a SNAPSHOT release instead (tag with -SNAPSHOT suffix)")
	return fmt.Errorf("%s: %w", b.String(), errs.ErrPermissionDenied)
}
