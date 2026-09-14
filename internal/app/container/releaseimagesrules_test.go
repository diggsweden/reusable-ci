// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"errors"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestValidateReleaseImagesPath_ConfinesTheLedgerUnderDist keeps the image
// ledger inside the directory the release is assembled from. Every refusal is
// checked against errs.ErrUsage rather than merely being non-nil: these inputs
// are caller mistakes, and the exit-code ladder depends on that distinction --
// a validator that started returning a different class here would still look
// correct to a bare nil-check.
func TestValidateReleaseImagesPath_ConfinesTheLedgerUnderDist(t *testing.T) {
	t.Parallel()

	if err := appcontainer.ValidateReleaseImagesPath("dist/release-images.json", "dist"); err != nil {
		t.Fatalf("valid confined path rejected: %v", err)
	}

	for name, path := range map[string]string{
		"outside dist":            "release-images.json",
		"dist itself":             "dist",
		"parent step out of dist": "dist/../release-images.json",
		"nested parent steps":     "dist/sub/../../release-images.json",
		// Refused since the '..' detection moved to pathsafe.Relative --
		// deliberate tightenings, pinned so they stay refused: control
		// characters have no legitimate producer in a CI flag value, and a
		// doubled slash makes the under-dist suffix read as absolute.
		"a tab in the name":          "dist/release\timages.json",
		"a trailing newline":         "dist/release-images.json\n",
		"a doubled slash after dist": "dist//release-images.json",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := appcontainer.ValidateReleaseImagesPath(path, "dist"); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("path %q: err = %v, want errs.ErrUsage", path, err)
			}
		})
	}
}

func TestDefaultReleaseImageRepository_LowercasesTheRepo(t *testing.T) {
	t.Parallel()

	got := appcontainer.DefaultReleaseImageRepository("codeberg.org", "Itiquette/Nanolinter")
	if want := "codeberg.org/itiquette/nanolinter"; got != want {
		t.Errorf("DefaultReleaseImageRepository() = %q, want %q", got, want)
	}
}

func TestRegistryHost_StripsSchemeAndPath(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ raw, want string }{
		"an https URL":             {raw: "https://codeberg.org", want: "codeberg.org"},
		"an http URL with a slash": {raw: "http://git.example.test/", want: "git.example.test"},
		"a repository path":        {raw: "registry.example.test/org/repo", want: "registry.example.test"},
		"surrounding whitespace":   {raw: "  codeberg.org  ", want: "codeberg.org"},
		"an explicit port":         {raw: "codeberg.org:5000", want: "codeberg.org:5000"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := appcontainer.RegistryHost(tc.raw)
			if err != nil {
				t.Fatalf("RegistryHost(%q): %v", tc.raw, err)
			}

			if got != tc.want {
				t.Errorf("RegistryHost(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRegistryHost_RefusesWithoutEchoingCredentials covers the refusals, and
// the property two of them used to break: every refusal quoted the raw input,
// so a server URL misconfigured as https://user:TOKEN@host put the token into
// the error and from there into a CI log. The canary must not appear in any
// refusal, whichever of the three parse paths it reached.
func TestRegistryHost_RefusesWithoutEchoingCredentials(t *testing.T) {
	t.Parallel()

	const canary = "s3cr3t-canary"

	for name, raw := range map[string]string{
		"a password in a URL":         "https://user:" + canary + "@codeberg.org",
		"a token as the URL username": "https://" + canary + "@codeberg.org/org/repo",
		"userinfo on a bare host":     "user:" + canary + "@codeberg.org",
		"userinfo on a repository":    canary + "@codeberg.org/org/repo",
		"another scheme":              "ftp://codeberg.org",
		"a scheme with no host":       "https://",
		"whitespace only":             "   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := appcontainer.RegistryHost(raw)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("RegistryHost(%q) = (%q, %v), want ErrUsage", raw, got, err)
			}

			if strings.Contains(err.Error(), canary) {
				t.Errorf("the refusal echoes the credential: %v", err)
			}

			// The wrapper names its context, so an operator can tell which of
			// several host inputs was refused.
			if !strings.HasPrefix(err.Error(), "release images: ") {
				t.Errorf("err = %v, want the release images prefix", err)
			}
		})
	}
}

// TestOptionalRegistryHost_OnlyEmptyIsOptional pins the wrapper's one rule:
// an absent value is allowed and yields no host, while any present value is
// validated in full. Whitespace is present, not absent -- a flag set to spaces
// is a mistake to report rather than an omission to accept.
func TestOptionalRegistryHost_OnlyEmptyIsOptional(t *testing.T) {
	t.Parallel()

	if got, err := appcontainer.OptionalRegistryHost(""); got != "" || err != nil {
		t.Errorf("empty = (%q, %v), want no host and no error", got, err)
	}

	if got, err := appcontainer.OptionalRegistryHost("   "); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("whitespace = (%q, %v), want ErrUsage", got, err)
	}

	if got, err := appcontainer.OptionalRegistryHost("https://codeberg.org/"); got != "codeberg.org" || err != nil {
		t.Errorf("present = (%q, %v), want codeberg.org", got, err)
	}
}

// TestNormalizedServerURL_KeepsTheSchemeIntact covers the normaliser feeding
// OptionalRegistryHost from --server-url, including the case it got wrong:
// "https://" had its scheme slashes trimmed into "https:", gained a second
// scheme, and reached the host parser as "https://https:", which it accepted as
// the registry "https:" with no error.
func TestNormalizedServerURL_KeepsTheSchemeIntact(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ raw, want string }{
		"a bare host gains https":      {raw: "codeberg.org", want: "https://codeberg.org"},
		"trailing slashes are trimmed": {raw: "https://codeberg.org//", want: "https://codeberg.org"},
		"an http scheme is kept":       {raw: " http://git.example.test/ ", want: "http://git.example.test"},
		"empty stays empty":            {raw: "", want: ""},
		"whitespace becomes empty":     {raw: "  ", want: ""},
		"a bare scheme stays bare":     {raw: "https://", want: "https://"},
		"a bare scheme with a slash":   {raw: "https:///", want: "https://"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := appcontainer.NormalizedServerURL(tc.raw); got != tc.want {
				t.Errorf("NormalizedServerURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	// And the composition the CLI performs refuses the bare scheme.
	if host, err := appcontainer.OptionalRegistryHost(appcontainer.NormalizedServerURL("https://")); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("--server-url https:// became host %q with err %v, want ErrUsage", host, err)
	}
}
