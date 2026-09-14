// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestResolveKeylessIdentity_GitLab asserts every field of the identity, not
// two of the four. TokenAudience and SubjectID were never read: the audience
// is what the runner requests when minting the token, so a wrong one yields a
// certificate no verifier accepts, and SubjectID is published as the exact
// identity for exact-match verification.
func TestResolveKeylessIdentity_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"GITLAB_CI":      "true",
			"CI_PROJECT_URL": "https://gitlab.com/grp/sub/proj",
			"CI_SERVER_URL":  "https://gitlab.com",
		}[k]
	}}

	if !p.SupportsKeyless() {
		t.Fatal("GitLab should support keyless")
	}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	want := provider.KeylessIdentity{
		OIDCIssuer:    "https://gitlab.com",
		TokenAudience: provider.KeylessAudience,
		SubjectID:     "https://gitlab.com/grp/sub/proj",
		SubjectRegexp: `^https://gitlab\.com/grp/sub/proj/`,
	}
	if id != want {
		t.Errorf("identity = %+v\nwant          %+v", id, want)
	}
}

// TestResolveKeylessIdentity_GitLab_NormalizesTheProjectURL pins the trailing
// slash. CI_PROJECT_URL is the runner's value and both spellings occur; an
// un-normalised one produces the anchor "…/proj//", which matches no
// certificate at all, so verification fails for a correctly signed artifact.
func TestResolveKeylessIdentity_GitLab_NormalizesTheProjectURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://gitlab.com/grp/sub/proj/",
		"https://gitlab.com/grp/sub/proj///",
		"  https://gitlab.com/grp/sub/proj  ",
	} {
		p := &gitlab.Provider{Env: func(k string) string {
			return map[string]string{"GITLAB_CI": "true", "CI_PROJECT_URL": raw, "CI_SERVER_URL": "https://gitlab.com"}[k]
		}}

		id, err := p.ResolveKeylessIdentity()
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}

		if want := `^https://gitlab\.com/grp/sub/proj/`; id.SubjectRegexp != want {
			t.Errorf("%q: SubjectRegexp = %q, want %q", raw, id.SubjectRegexp, want)
		}
	}
}

// TestResolveKeylessIdentity_GitLab_AnchorAcceptsOnlyThisProject is the
// behaviour behind the literal above.
//
// The anchor decides which certificates a verifier accepts. Comparing it as a
// string proves it was built, not that it draws the boundary anywhere useful,
// and the failure that matters is silent: an anchor that also matches a
// sibling project means any pipeline in the group can produce a signature that
// verifies as this project's.
//
// The metacharacter quoting is part of that. A project path is attacker-
// influenced to the extent that anyone who can create a group can choose one,
// and an unquoted "." in the host would let gitlabXcom match.
func TestResolveKeylessIdentity_GitLab_AnchorAcceptsOnlyThisProject(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"GITLAB_CI":      "true",
			"CI_PROJECT_URL": "https://gitlab.com/grp/sub/proj",
			"CI_SERVER_URL":  "https://gitlab.com",
		}[k]
	}}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	anchor, err := regexp.Compile(id.SubjectRegexp)
	if err != nil {
		t.Fatalf("SubjectRegexp does not compile, so no verifier can use it: %v", err)
	}

	for name, san := range map[string]bool{
		// A real GitLab SAN is the CI config path plus the ref.
		"this project's pipeline":     true,
		"another ref of this project": true,
		"a sibling project":           false,
		"a prefix-sharing project":    false,
		"the project itself, no path": false,
		"a lookalike host":            false,
		"the project under a path":    false,
	} {
		sanValue := map[string]string{
			"this project's pipeline":     "https://gitlab.com/grp/sub/proj//.gitlab-ci.yml@refs/heads/main",
			"another ref of this project": "https://gitlab.com/grp/sub/proj//.gitlab-ci.yml@refs/tags/v1.0.0",
			"a sibling project":           "https://gitlab.com/grp/sub/other//.gitlab-ci.yml@refs/heads/main",
			"a prefix-sharing project":    "https://gitlab.com/grp/sub/proj-evil//.gitlab-ci.yml@refs/heads/main",
			"the project itself, no path": "https://gitlab.com/grp/sub/proj",
			"a lookalike host":            "https://gitlabXcom/grp/sub/proj//.gitlab-ci.yml@refs/heads/main",
			"the project under a path":    "https://evil.example/https://gitlab.com/grp/sub/proj//.gitlab-ci.yml",
		}[name]

		if got := anchor.MatchString(sanValue); got != san {
			t.Errorf("%s: anchor.MatchString(%q) = %v, want %v", name, sanValue, got, san)
		}
	}
}

func TestResolveKeylessIdentity_GitLab_MissingProjectURL(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing CI_PROJECT_URL should be ErrUsage, got %v", err)
	}
}
