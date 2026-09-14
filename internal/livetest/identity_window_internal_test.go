// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// The expectations in this file are written out as literals. The destructive
// guard compares an operator's confirmation with Identity(), and a fixture that
// derived its expected identity from Identity() would agree with any change to
// it.

func TestIdentity_ExactStatedValuesAndRefusals(t *testing.T) {
	t.Parallel()

	forgejo := targetRef{forge: "forgejo", host: "forgejo.compose.forgelab:8443", owner: "garga"}
	gitlab := targetRef{forge: string(provider.ForgeGitLab), host: "gitlab.k3s.forgelab", owner: "Bot_1.x"}
	gitea := targetRef{forge: "gitea", host: "gitea.compose.forgelab:8443", owner: "garga"}

	for name, tc := range map[string]struct {
		refs []targetRef
		want string
	}{
		"single forge": {refs: []targetRef{forgejo}, want: "run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-"},
		"two forges in producer order": {refs: []targetRef{gitlab, forgejo},
			want: "run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-,gitlab@https://gitlab.k3s.forgelab/Bot_1.x#resources=rc-"},
		"two forges reversed": {refs: []targetRef{forgejo, gitlab},
			want: "run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-,gitlab@https://gitlab.k3s.forgelab/Bot_1.x#resources=rc-"},
		"gitea is confirmed too": {refs: []targetRef{gitea, forgejo},
			want: "run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-,gitea@https://gitea.compose.forgelab:8443/garga#resources=rc-"},
	} {
		if got, err := Identity("run-live-1", tc.refs, ResourcePrefix); err != nil || got != tc.want {
			t.Errorf("%s: Identity = %q, %v, want %q", name, got, err, tc.want)
		}
	}

	with := func(ref targetRef, change func(*targetRef)) targetRef {
		change(&ref)

		return ref
	}

	for name, tc := range map[string]struct {
		runID  string
		refs   []targetRef
		reason string
	}{
		"no provider":             {runID: "run-live-1", reason: "no provider is selected"},
		"repeated provider":       {runID: "run-live-1", refs: []targetRef{forgejo, with(forgejo, func(r *targetRef) { r.owner = "other" })}, reason: "empty or repeated"},
		"empty provider":          {runID: "run-live-1", refs: []targetRef{with(forgejo, func(r *targetRef) { r.forge = "" })}, reason: "empty or repeated"},
		"github is not a target":  {runID: "run-live-1", refs: []targetRef{with(forgejo, func(r *targetRef) { r.forge = "github" })}, reason: `unknown provider "github"`},
		"real forge host":         {runID: "run-live-1", refs: []targetRef{with(forgejo, func(r *targetRef) { r.host = "codeberg.org" })}, reason: "is not a disposable lab forge"},
		"owner parent directory":  {runID: "run-live-1", refs: []targetRef{forgejo, with(gitea, func(r *targetRef) { r.owner = ".." })}, reason: `gitea owner ".." is not a resource owner`},
		"owner current directory": {runID: "run-live-1", refs: []targetRef{with(gitlab, func(r *targetRef) { r.owner = "." })}, reason: `gitlab owner "." is not a resource owner`},
		"owner path":              {runID: "run-live-1", refs: []targetRef{with(forgejo, func(r *targetRef) { r.owner = "garga/other" })}, reason: "is not a resource owner"},
		"upper-case run":          {runID: "Run-live-1", refs: []targetRef{forgejo}, reason: "is invalid"},
		"short run":               {runID: "ru", refs: []targetRef{forgejo}, reason: "is invalid"},
	} {
		if got, err := Identity(tc.runID, tc.refs, ResourcePrefix); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: Identity = %q, %v, want a refusal mentioning %q", name, got, err, tc.reason)
		}
	}
}

// TestValidateAuthorization_BindsTheExactTargetAndConfirmation holds the acted-on
// target to its own identity entry and the confirmation to the exact stated
// string, independent of the order the producer listed the endpoints in.
func TestValidateAuthorization_BindsTheExactTargetAndConfirmation(t *testing.T) {
	t.Parallel()

	const confirmation = "destroy-live-forge-fixtures|run=run-live-1|targets=" +
		"forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-," +
		"gitlab@https://gitlab.compose.forgelab:8443/garga#resources=rc-"

	refs := []targetRef{
		{forge: string(provider.ForgeGitLab), host: "gitlab.compose.forgelab:8443", owner: "garga"},
		{forge: "forgejo", host: "forgejo.compose.forgelab:8443", owner: "garga"},
	}
	target := Target{Forge: provider.ForgeForgejo, Host: "forgejo.compose.forgelab:8443", Owner: "garga"}
	sourced := contract{runID: "run-live-1", refs: refs, resourcePrefix: ResourcePrefix, confirmation: confirmation}

	if err := validateAuthorization(target, sourced); err != nil {
		t.Fatalf("stated confirmation rejected: %v", err)
	}

	for name, tc := range map[string]struct {
		target       Target
		confirmation string
		reason       string
	}{
		"target owner not the confirmed owner": {target: Target{Forge: provider.ForgeForgejo, Host: "forgejo.compose.forgelab:8443", Owner: "other"}, confirmation: confirmation, reason: "does not contain the exact target"},
		"target port not the confirmed port":   {target: Target{Forge: provider.ForgeForgejo, Host: "forgejo.compose.forgelab", Owner: "garga"}, confirmation: confirmation, reason: "does not contain the exact target"},
		"target forge not armed":               {target: Target{Forge: "gitea", Host: "gitea.compose.forgelab:8443", Owner: "garga"}, confirmation: confirmation, reason: "does not contain the exact target"},
		"confirmation in producer order": {target: target, reason: "must equal", confirmation: "destroy-live-forge-fixtures|run=run-live-1|targets=" +
			"gitlab@https://gitlab.compose.forgelab:8443/garga#resources=rc-,forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-"},
		"confirmation with a trailing newline": {target: target, confirmation: confirmation + "\n", reason: "must equal"},
		"confirmation for one forge only": {target: target, reason: "must equal",
			confirmation: "destroy-live-forge-fixtures|run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=rc-"},
	} {
		changed := sourced
		changed.confirmation = tc.confirmation

		if err := validateAuthorization(tc.target, changed); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: %v, want a refusal mentioning %q", name, err, tc.reason)
		}
	}
}

// TestValidateTokenMetadata_WindowBoundaries walks each lifetime limit to the
// second on both sides, for the revocable Forgejo form and the natively
// expiring GitLab form, and every revocation combination that contradicts the
// form.
func TestValidateTokenMetadata_WindowBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	stamp := func(offset time.Duration) string { return now.Add(offset).Format(time.RFC3339) }
	revocable := tokenMetadata{id: "7", name: "lab-targets-run-live-1", createdAt: stamp(-time.Minute), revocationRequired: true, revocationRunID: "run-live-1"}
	expiring := tokenMetadata{id: "8", name: "lab-targets-run-live-1", createdAt: stamp(-time.Minute), expiresAt: stamp(24 * time.Hour)}

	for name, tc := range map[string]struct {
		forge  provider.ForgeAPI
		token  tokenMetadata
		change func(*tokenMetadata)
		reason string
	}{
		"revocable at exactly 24 hours":       {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = stamp(-24 * time.Hour) }},
		"revocable one second past 24 hours":  {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = stamp(-24*time.Hour - time.Second) }, reason: "older than 24 hours"},
		"created at the skew limit":           {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = stamp(5 * time.Minute) }},
		"created one second past the skew":    {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = stamp(5*time.Minute + time.Second) }, reason: "in the future"},
		"created as a date only":              {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = "2026-07-25" }, reason: "must be RFC3339"},
		"created with an offset":              {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.createdAt = "2026-07-25T13:59:00+02:00" }},
		"token id zero":                       {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.id = "0" }, reason: "must be numeric"},
		"token id with a leading zero":        {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.id = "07" }, reason: "must be numeric"},
		"revocation for another run":          {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.revocationRunID = "run-live-2" }, reason: "requires run-bound revocation"},
		"revocation not required":             {forge: provider.ForgeForgejo, token: revocable, change: func(tk *tokenMetadata) { tk.revocationRequired = false }, reason: "requires run-bound revocation"},
		"gitlab revocable form":               {forge: provider.ForgeGitLab, token: revocable},
		"expiry at exactly the skew":          {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = stamp(5 * time.Minute) }, reason: "between 5 minutes and 31 days"},
		"expiry one second past the skew":     {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = stamp(5*time.Minute + time.Second) }},
		"expiry at exactly 31 days":           {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = stamp(31 * 24 * time.Hour) }},
		"expiry one second past 31 days":      {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = stamp(31*24*time.Hour + time.Second) }, reason: "between 5 minutes and 31 days"},
		"expiry as a date":                    {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = "2026-08-01" }},
		"expiry already past":                 {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = "2026-07-25" }, reason: "between 5 minutes and 31 days"},
		"expiry unparsable":                   {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.expiresAt = "soon" }, reason: "token expiry"},
		"expiring with revocation required":   {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.revocationRequired = true }, reason: "conflicts with revocation"},
		"expiring with a revocation run":      {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.revocationRunID = "run-live-1" }, reason: "conflicts with revocation"},
		"forgejo with an expiry":              {forge: provider.ForgeForgejo, token: expiring, reason: "has no native token expiry"},
		"expiring token created ten days ago": {forge: provider.ForgeGitLab, token: expiring, change: func(tk *tokenMetadata) { tk.createdAt = stamp(-10 * 24 * time.Hour) }},
	} {
		token := tc.token
		if tc.change != nil {
			tc.change(&token)
		}

		err := validateTokenMetadata(tc.forge, "run-live-1", token, now)

		switch {
		case tc.reason == "" && err != nil:
			t.Errorf("%s: rejected: %v", name, err)
		case tc.reason != "" && (!errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), tc.reason)):
			t.Errorf("%s: %v, want a refusal mentioning %q", name, err, tc.reason)
		}
	}
}
