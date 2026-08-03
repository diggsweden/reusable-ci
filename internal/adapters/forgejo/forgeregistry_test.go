// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestResolveForgeMavenRegistry_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{"FORGEJO_SERVER_URL": "https://codeberg.org", "FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_TOKEN": "ft"}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://codeberg.org/api/packages/owner/maven" || reg.AuthScheme != provider.MavenAuthTokenHeader || reg.Token != "ft" {
		t.Errorf("registry = %+v", reg)
	}
}

// TestResolveForgeMavenRegistry_ForgeNeutralNames pins the fix that moving
// the name lists into runcontext bought: a workflow that exports the
// forge-neutral names is understood by the package-registry adapter, not
// just by the commands whose flags bind to cienv.
//
// Before, this adapter resolved from its own inline
// ("FORGEJO_SERVER_URL", "GITHUB_SERVER_URL") list, so exporting the
// documented $CI_SERVER_URL / $CI_REPO pair failed here with "required"
// while `release publish` resolved the very same run fine.
func TestResolveForgeMavenRegistry_ForgeNeutralNames(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"CI_SERVER_URL": "https://codeberg.org",
			"CI_REPO":       "owner/repo",
			"CI_TOKEN":      "ct",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://codeberg.org/api/packages/owner/maven" || reg.Token != "ct" {
		t.Errorf("registry = %+v", reg)
	}
}

// TestResolveForgeMavenRegistry_CrossForgeTokenNotLeaked pins the fix for a
// PROVEN disclosure: a GitHub-hosted repo publishing to a third-party
// Forgejo instance, with FORGEJO_TOKEN empty because the secret is unset (it
// interpolates to "", and empty means absent, so the chain fell through).
//
// The old chain ended in GITHUB_TOKEN, so it handed the GitHub job token to
// third-party.example. A GitHub token cannot authenticate at Forgejo, so the
// fallback could only ever fail or disclose. Resolving "" makes the run fail
// loudly at auth instead.
func TestResolveForgeMavenRegistry_CrossForgeTokenNotLeaked(t *testing.T) {
	t.Parallel()

	//nolint:gosec // G101: fixture values, not credentials; the literal IS the assertion.
	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":     "true", // executing on a GitHub runner
			"FORGEJO_SERVER_URL": "https://third-party.example",
			"FORGEJO_REPOSITORY": "owner/repo",
			"FORGEJO_TOKEN":      "",
			"GITHUB_TOKEN":       "ghs_REAL_GITHUB_JOB_TOKEN",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Token != "" {
		t.Errorf("token sent to %s = %q; a GitHub job token must never be"+
			" transmitted to a Forgejo host", reg.URL, reg.Token)
	}
}

// TestResolveForgeMavenRegistry_ForgejoRunnerGithubTokenAlias is the other
// half: on a REAL Forgejo runner, act_runner exposes the job token under the
// GitHub-compatible name, so there $GITHUB_TOKEN IS the Forgejo credential
// and must keep working. Gating on runner identity is what tells them apart.
func TestResolveForgeMavenRegistry_ForgejoRunnerGithubTokenAlias(t *testing.T) {
	t.Parallel()

	//nolint:gosec // G101: fixture values, not credentials; the literal IS the assertion.
	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":     "true",
			"FORGEJO_ACTIONS":    "true", // ...but a Forgejo runner
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_REPOSITORY": "owner/repo",
			"GITHUB_TOKEN":       "forgejo-job-token-under-compat-name",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Token != "forgejo-job-token-under-compat-name" {
		t.Errorf("token = %q; the Forgejo runner's $GITHUB_TOKEN alias must still work", reg.Token)
	}
}

// TestResolveForgeMavenRegistry_GiteaToken guards the one name that folding
// this adapter's inline list into the shared chain could have dropped:
// GITEA_TOKEN was honoured here and nowhere else, so it had to move INTO
// runcontext.Token rather than be lost to the migration.
func TestResolveForgeMavenRegistry_GiteaToken(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_REPOSITORY": "owner/repo",
			"GITEA_TOKEN":        "gt",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Token != "gt" {
		t.Errorf("token = %q, want the $GITEA_TOKEN value", reg.Token)
	}
}

// TestResolveForgeMavenRegistry_MissingNamesAreListed checks the diagnostic
// names every variable that would have worked. The old message named a
// single one ($FORGEJO_SERVER_URL), which sent a caller to set a
// forge-specific var when the neutral one was the better answer.
func TestResolveForgeMavenRegistry_MissingNamesAreListed(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(_ string) string { return "" }}

	_, err := p.ResolveForgeMavenRegistry()
	if err == nil {
		t.Fatal("want an error when the run context carries no server url")
	}

	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want errs.ErrUsage", err)
	}

	for _, name := range []string{"$CI_SERVER_URL", "$FORGEJO_SERVER_URL", "$GITHUB_SERVER_URL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("err = %q, want it to list %s", err, name)
		}
	}
}

func TestResolveForgeNPMRegistry_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{"FORGEJO_SERVER_URL": "https://codeberg.org", "FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_TOKEN": "ft"}[k]
	}}

	reg, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Registry != "https://codeberg.org/api/packages/owner/npm/" || reg.Scope != "@owner" {
		t.Errorf("npm registry = %+v", reg)
	}
}
