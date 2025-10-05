// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestReleaseProvider_RefusesGHESAndAcceptsHostedGitHub(t *testing.T) {
	t.Parallel()

	if err := appvalidate.ReleaseProvider(provider.ForgeGitHub, "https://github.com"); err != nil {
		t.Fatal(err)
	}

	if err := appvalidate.ReleaseProvider(provider.ForgeGitHub, "https://github.example.test"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("GHES err = %v, want ErrUnsupported", err)
	}

	if err := appvalidate.ReleaseProvider(provider.ForgeForgejo, "https://github.example.test"); err != nil {
		t.Fatalf("Forgejo was treated as GHES: %v", err)
	}
}

// TestReleaseProvider_ClassifiesTheServerURLWithoutEchoingIt is the matrix the
// three-case test above did not cover, separating normalisation from the
// deliberate bypasses and from refusals.
//
// Normalisation: case and surrounding whitespace do not change which host is
// meant. Bypasses: a non-GitHub forge and an unset URL are not this check's
// business. Refusals: a value that is not an absolute URL is a configuration
// error, and a host that is not exactly github.com -- including one that merely
// starts with it -- is an unsupported instance.
//
// No refusal may contain the canary. The malformed branch used to quote the
// raw value, so a broken URL carrying a token in its userinfo put the token in
// the CI log.
func TestReleaseProvider_ClassifiesTheServerURLWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	const canary = "s3cr3t-canary"

	for name, tc := range map[string]struct {
		forge provider.ForgeAPI
		url   string
		want  error
	}{
		"uppercase host":                     {forge: provider.ForgeGitHub, url: "https://GITHUB.COM"},
		"surrounding whitespace and slash":   {forge: provider.ForgeGitHub, url: "  https://github.com/  "},
		"unset URL is not applicable":        {forge: provider.ForgeGitHub, url: ""},
		"another forge is not applicable":    {forge: provider.ForgeGitLab, url: "not a url"},
		"no scheme":                          {forge: provider.ForgeGitHub, url: "github.com", want: errs.ErrInvalidConfig},
		"not a URL":                          {forge: provider.ForgeGitHub, url: "not a url", want: errs.ErrInvalidConfig},
		"a broken URL carrying a token":      {forge: provider.ForgeGitHub, url: "https://user:" + canary + "@%zz", want: errs.ErrInvalidConfig},
		"a schemeless URL carrying a token":  {forge: provider.ForgeGitHub, url: "://" + canary + "@x", want: errs.ErrInvalidConfig},
		"GHES with credentials":              {forge: provider.ForgeGitHub, url: "https://user:" + canary + "@github.example.test", want: errs.ErrUnsupported},
		"a host that starts with github.com": {forge: provider.ForgeGitHub, url: "https://github.com.evil.example", want: errs.ErrUnsupported},
		"github.com only in the path":        {forge: provider.ForgeGitHub, url: "https://evil.example/github.com", want: errs.ErrUnsupported},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := appvalidate.ReleaseProvider(tc.forge, tc.url)
			if tc.want == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}

				return
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if strings.Contains(err.Error(), canary) {
				t.Errorf("the refusal echoes a credential: %v", err)
			}
		})
	}
}
