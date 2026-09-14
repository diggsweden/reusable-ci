// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// TestValidators_BlankInputsAreMissing states the normalization policy for the
// secrets and identifiers the prerequisite validators accept: a value that is
// empty or only whitespace is missing, refused with the class an empty value
// gets and before any provider call or success output. It used to be present,
// so a blank token or repository reached the provider probe and a blank Maven
// Central secret or GPG key passed its check. A non-blank value is not trimmed
// or otherwise changed; the provider still decides whether it is valid.
func TestValidators_BlankInputsAreMissing(t *testing.T) {
	t.Parallel()

	type run func(prov *fakeprovider.Fake, value string, out *bytes.Buffer) error

	for _, tc := range []struct {
		name string
		run  run
		want error
	}{
		{"token", func(prov *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.Token(context.Background(), prov, out, appvalidate.TokenInput{Token: value, Repository: "owner/repo"})
		}, errs.ErrPermissionDenied},
		{"token repository", func(prov *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.Token(context.Background(), prov, out, appvalidate.TokenInput{Token: "github_pat_AAAA", Repository: value}) //nolint:gosec // synthetic token.
		}, errs.ErrUsage},
		{"bot permissions repository", func(prov *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.BotPermissions(context.Background(), prov, out, appvalidate.BotPermissionsInput{Repository: value})
		}, errs.ErrUsage},
		{"maven central username", func(_ *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.MavenCentralCredentials(out, out, output.Annotator{}, appvalidate.MavenCentralCredentialsInput{Username: value, Password: "secret"})
		}, errs.ErrPermissionDenied},
		{"maven central password", func(_ *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.MavenCentralCredentials(out, out, output.Annotator{}, appvalidate.MavenCentralCredentialsInput{Username: "user", Password: value})
		}, errs.ErrPermissionDenied},
		{"release gpg public key", func(_ *fakeprovider.Fake, value string, out *bytes.Buffer) error {
			return appvalidate.GPGPublicKey(out, value)
		}, errs.ErrPermissionDenied},
	} {
		for _, value := range []string{"", " ", "\t\n", "  "} {
			t.Run(tc.name+"/"+value, func(t *testing.T) {
				t.Parallel()

				prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithBotPermissions(provider.BotPermissions{
					UserAccessible: true, RepoAccessible: true, BranchesAccessible: true,
				})

				var out bytes.Buffer

				err := tc.run(prov, value, &out)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}

				if calls := prov.Calls(); calls.ValidateToken != 0 || calls.ValidateBotPermissions != 0 {
					t.Errorf("provider called for a blank value: %+v", calls)
				}

				if bytes.Contains(out.Bytes(), []byte("✓")) || bytes.Contains(out.Bytes(), []byte("validated")) || bytes.Contains(out.Bytes(), []byte("configured")) {
					t.Errorf("a blank value reported success: %q", out.String())
				}
			})
		}
	}

	t.Run("a padded token reaches the provider unchanged", func(t *testing.T) {
		t.Parallel()

		prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

		if err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{Token: " github_pat_AAAA ", Repository: "owner/repo"}); err != nil { //nolint:gosec // synthetic token.
			t.Fatal(err)
		}

		if calls := prov.ValidateTokenCalls(); len(calls) != 1 || calls[0].Token != " github_pat_AAAA " {
			t.Errorf("provider calls = %+v, want the value exactly as given", calls)
		}
	})
}
